using Amazon.Lambda.Core;

[assembly: LambdaSerializer(typeof(Amazon.Lambda.Serialization.SystemTextJson.DefaultLambdaJsonSerializer))]

namespace GreetingFunction;

/// <summary>
/// Lambda handlers for the Greeting service.
///
/// Handler strings (for .lambit.toml):
///   GreetingFunction::GreetingFunction.Function::Greet
///   GreetingFunction::GreetingFunction.Function::GreetFormal
///   GreetingFunction::GreetingFunction.Function::Transform
/// </summary>
public class Function
{
    private static readonly Dictionary<string, string> _templates = new(StringComparer.OrdinalIgnoreCase)
    {
        ["en"] = "Hello, {0}!",
        ["es"] = "¡Hola, {0}!",
        ["fr"] = "Bonjour, {0}!",
        ["de"] = "Hallo, {0}!",
        ["ja"] = "こんにちは、{0}！",
    };

    /// <summary>
    /// Returns a friendly greeting in the requested language.
    /// Exercises: multiple test cases, [Theory] with InlineData, data models.
    /// </summary>
    public GreetingResponse Greet(GreetingRequest request, ILambdaContext context)
    {
        context?.Logger.LogInformation($"Greet called: name={request.Name} lang={request.Language}");

        var template = _templates.TryGetValue(request.Language, out var t)
            ? t
            : _templates["en"];

        var message = string.Format(template, request.Name);
        return new GreetingResponse
        {
            Message  = message,
            Language = request.Language,
            Length   = message.Length,
        };
    }

    /// <summary>
    /// Returns a formal greeting — always in English, with a title prefix.
    /// Exercises: separate handler with its own test cases.
    /// </summary>
    public GreetingResponse GreetFormal(GreetingRequest request, ILambdaContext context)
    {
        context?.Logger.LogInformation($"GreetFormal called: name={request.Name}");

        var message = $"Good day, {request.Name}. It is a pleasure to make your acquaintance.";
        return new GreetingResponse
        {
            Message  = message,
            Language = "en",
            Length   = message.Length,
        };
    }

    /// <summary>
    /// Transforms text using the specified operation.
    /// Exercises: [Theory] tests across multiple operations, error handling.
    /// </summary>
    public TransformResponse Transform(TransformRequest request, ILambdaContext context)
    {
        context?.Logger.LogInformation($"Transform called: op={request.Operation}");

        if (string.IsNullOrWhiteSpace(request.Text))
            throw new ArgumentException("Text must not be empty.");

        var transformed = request.Operation.ToLowerInvariant() switch
        {
            "upper"   => request.Text.ToUpperInvariant(),
            "lower"   => request.Text.ToLowerInvariant(),
            "reverse" => new string(request.Text.Reverse().ToArray()),
            "shout"   => request.Text.ToUpperInvariant() + "!!!",
            _         => throw new ArgumentException($"Unknown operation: {request.Operation}"),
        };

        return new TransformResponse
        {
            Original    = request.Text,
            Transformed = transformed,
            Operation   = request.Operation,
        };
    }
}
