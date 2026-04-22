namespace GreetingFunction;

/// <summary>Input for the greeting functions.</summary>
public class GreetingRequest
{
    public string Name    { get; set; } = "World";
    public string Language { get; set; } = "en";
}

/// <summary>Response returned by the greeting functions.</summary>
public class GreetingResponse
{
    public string Message  { get; set; } = string.Empty;
    public string Language { get; set; } = string.Empty;
    public int    Length   { get; set; }
}

/// <summary>Input for the text transformation function.</summary>
public class TransformRequest
{
    public string Text      { get; set; } = string.Empty;
    public string Operation { get; set; } = "upper"; // upper | lower | reverse | shout
}

/// <summary>Response from the text transformation function.</summary>
public class TransformResponse
{
    public string Original    { get; set; } = string.Empty;
    public string Transformed { get; set; } = string.Empty;
    public string Operation   { get; set; } = string.Empty;
}
