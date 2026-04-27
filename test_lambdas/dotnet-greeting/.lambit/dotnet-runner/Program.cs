// lambit .NET runner shim — dotnet-greeting edition
// Usage: dotnet run --project <this-dir> -- <handler> <json-payload>

using System.Reflection;
using System.Runtime.Loader;
using System.Text.Json;

if (args.Length < 2)
{
    Console.Error.WriteLine("Usage: LambitRunner <handler> <json-payload>");
    return 1;
}

var handler = args[0];
var payload = args[1];

var parts = handler.Split("::");
if (parts.Length != 3)
{
    Console.Error.WriteLine($"Invalid handler format: {handler}");
    return 1;
}

var assemblyName = parts[0];
var typeName     = parts[1];
var methodName   = parts[2];

// The invoker (invoke.go) sets cmd.Dir to the lambda project root, so the
// process working directory IS the project root — use it directly.
var projectRoot = Directory.GetCurrentDirectory();
var lambitDir   = Path.Combine(projectRoot, ".lambit");

// Prefer the published output so all dependencies are available.
var publishOut = Path.Combine(lambitDir, "lambda-out");
var searchDirs = new[]
{
    publishOut,                                     // .lambit/lambda-out/  (preferred)
    Path.Combine(projectRoot, "src"),               // <root>/src/**
    Path.Combine(projectRoot, "bin"),               // <root>/bin/**
    projectRoot,
};

string? dllPath = null;
foreach (var dir in searchDirs)
{
    if (!Directory.Exists(dir)) continue;
    var found = Directory.GetFiles(dir, $"{assemblyName}.dll", SearchOption.AllDirectories);
    if (found.Length > 0) { dllPath = found[0]; break; }
}

if (dllPath == null)
{
    Console.Error.WriteLine($"Could not find '{assemblyName}.dll'");
    Console.Error.WriteLine($"Searched: {string.Join(", ", searchDirs.Where(Directory.Exists))}");
    Console.Error.WriteLine($"Run 'dotnet publish src/<Project> -o .lambit/lambda-out' to fix this.");
    return 1;
}

// Load the assembly from its directory so sibling deps resolve automatically.
var loadDir  = Path.GetDirectoryName(dllPath)!;
var loadCtx  = new PathAssemblyLoadContext(loadDir);
var assembly = loadCtx.LoadFromAssemblyPath(dllPath);

var type = assembly.GetType(typeName);
if (type == null)
{
    Console.Error.WriteLine($"Type '{typeName}' not found in '{assemblyName}'");
    return 1;
}

var method = type.GetMethod(methodName,
    BindingFlags.Public | BindingFlags.Instance | BindingFlags.Static);
if (method == null)
{
    Console.Error.WriteLine($"Method '{methodName}' not found on '{typeName}'");
    return 1;
}

object? instance = method.IsStatic ? null : Activator.CreateInstance(type);

var parameters = method.GetParameters();
object?[] invokeArgs;

if (parameters.Length == 0)
{
    invokeArgs = Array.Empty<object?>();
}
else
{
    var paramType = parameters[0].ParameterType;
    object? deserialized;
    try   { deserialized = JsonSerializer.Deserialize(payload, paramType, new JsonSerializerOptions { PropertyNameCaseInsensitive = true }); }
    catch (Exception e)
    {
        Console.Error.WriteLine($"Could not deserialize payload as {paramType.Name}: {e.Message}");
        return 1;
    }
    invokeArgs = parameters.Length == 1
        ? new object?[] { deserialized }
        : new object?[] { deserialized, null }; // ILambdaContext = null
}

try
{
    var result = method.Invoke(instance, invokeArgs);
    if (result is Task task)
    {
        await task;
        var tp = task.GetType();
        result = tp.IsGenericType ? tp.GetProperty("Result")?.GetValue(task) : null;
    }
    Console.WriteLine(JsonSerializer.Serialize(result,
        new JsonSerializerOptions { WriteIndented = true }));
}
catch (TargetInvocationException tie)
{
    Console.Error.WriteLine(tie.InnerException?.Message ?? tie.Message);
    return 1;
}
catch (Exception e)
{
    Console.Error.WriteLine(e.Message);
    return 1;
}

return 0;

// ─── Custom AssemblyLoadContext ───────────────────────────────────────────────
// Loads assemblies from a specific directory, falling back to the default context.
class PathAssemblyLoadContext(string baseDir) : AssemblyLoadContext(isCollectible: false)
{
    protected override Assembly? Load(AssemblyName name)
    {
        var candidate = Path.Combine(baseDir, name.Name + ".dll");
        return File.Exists(candidate) ? LoadFromAssemblyPath(candidate) : null;
    }
}
