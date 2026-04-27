// lambit .NET runner shim
// Usage: dotnet run --project <this-dir> -- <handler> <json-payload>
//
// handler format: "Assembly::Namespace.ClassName::MethodName"
//
// The shim reflects over the user's lambda assembly (expected to be in the
// parent directory of this project), instantiates the handler class, calls
// the method with a deserialized payload, and writes the JSON result to stdout.

using System.Reflection;
using System.Text.Json;
using System.Text.Json.Nodes;

if (args.Length < 2)
{
    Console.Error.WriteLine("Usage: LambitRunner <handler> <json-payload>");
    return 1;
}

var handler = args[0];
var payload = args[1];

// Parse handler string: "Assembly::Namespace.Class::Method"
var parts = handler.Split("::");
if (parts.Length != 3)
{
    Console.Error.WriteLine($"Invalid handler format: {handler}");
    Console.Error.WriteLine("Expected: Assembly::Namespace.ClassName::MethodName");
    return 1;
}

var assemblyName = parts[0];
var typeName = parts[1];
var methodName = parts[2];

// The invoker (invoke.go) sets cmd.Dir to the lambda project root, so the
// process working directory IS the project root — use it directly instead of
// navigating from AppContext.BaseDirectory which resolves to the shim's own
// bin/Debug/<tfm>/ directory and would give the wrong path.
var projectRoot = Directory.GetCurrentDirectory();
var buildOutput = Path.Combine(projectRoot, "bin", "Debug");

Assembly? assembly = null;

// Search common build output directories.
foreach (var searchDir in new[] { buildOutput, projectRoot })
{
    if (!Directory.Exists(searchDir)) continue;
    foreach (var dll in Directory.GetFiles(searchDir, $"{assemblyName}.dll", SearchOption.AllDirectories))
    {
        try
        {
            assembly = Assembly.LoadFrom(dll);
            break;
        }
        catch { }
    }
    if (assembly != null) break;
}

if (assembly == null)
{
    Console.Error.WriteLine($"Could not find assembly '{assemblyName}.dll' under {projectRoot}");
    return 1;
}

var type = assembly.GetType(typeName);
if (type == null)
{
    Console.Error.WriteLine($"Could not find type '{typeName}' in assembly '{assemblyName}'");
    return 1;
}

var method = type.GetMethod(methodName, BindingFlags.Public | BindingFlags.Instance | BindingFlags.Static);
if (method == null)
{
    Console.Error.WriteLine($"Could not find method '{methodName}' on type '{typeName}'");
    return 1;
}

// Instantiate handler class (needs parameterless constructor).
object? instance = null;
if (!method.IsStatic)
{
    instance = Activator.CreateInstance(type);
    if (instance == null)
    {
        Console.Error.WriteLine($"Could not instantiate type '{typeName}'");
        return 1;
    }
}

// Deserialize the payload into the first parameter type.
var parameters = method.GetParameters();
object?[] invokeArgs;

if (parameters.Length == 0)
{
    invokeArgs = Array.Empty<object?>();
}
else if (parameters.Length == 1)
{
    var paramType = parameters[0].ParameterType;
    try
    {
        var deserialized = JsonSerializer.Deserialize(payload, paramType);
        invokeArgs = new[] { deserialized };
    }
    catch (Exception e)
    {
        Console.Error.WriteLine($"Could not deserialize payload as {paramType.Name}: {e.Message}");
        return 1;
    }
}
else
{
    // Two-param signature: (TEvent input, ILambdaContext context) — pass null for context.
    var paramType = parameters[0].ParameterType;
    object? deserialized;
    try
    {
        deserialized = JsonSerializer.Deserialize(payload, paramType);
    }
    catch (Exception e)
    {
        Console.Error.WriteLine($"Could not deserialize payload as {paramType.Name}: {e.Message}");
        return 1;
    }
    invokeArgs = new object?[] { deserialized, null };
}

try
{
    var result = method.Invoke(instance, invokeArgs);
    // If the return type is Task or Task<T>, await it.
    if (result is Task task)
    {
        await task;
        var taskType = task.GetType();
        if (taskType.IsGenericType)
        {
            var resultProp = taskType.GetProperty("Result");
            result = resultProp?.GetValue(task);
        }
        else
        {
            result = null;
        }
    }
    Console.WriteLine(JsonSerializer.Serialize(result, new JsonSerializerOptions { WriteIndented = true }));
}
catch (Exception e)
{
    Console.Error.WriteLine(e.InnerException?.Message ?? e.Message);
    return 1;
}

return 0;
