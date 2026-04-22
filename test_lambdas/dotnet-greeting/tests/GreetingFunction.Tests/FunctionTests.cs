using Xunit;
using GreetingFunction;

namespace GreetingFunction.Tests;

/// <summary>
/// Unit tests for Function.Greet — exercises lambit's [Fact] and [Theory] discovery.
/// </summary>
public class FunctionTests
{
    private readonly Function _sut = new();

    // ── Greet ──────────────────────────────────────────────────────────────

    [Fact]
    public void Greet_WithDefaultRequest_ReturnsEnglishGreeting()
    {
        var result = _sut.Greet(new GreetingRequest(), null!);

        Assert.Equal("Hello, World!", result.Message);
        Assert.Equal("en", result.Language);
        Assert.Equal(13, result.Length);
    }

    [Fact]
    public void Greet_WithCustomName_IncludesNameInMessage()
    {
        var result = _sut.Greet(new GreetingRequest { Name = "Lambit" }, null!);

        Assert.Contains("Lambit", result.Message);
    }

    [Theory]
    [InlineData("en", "Hello, Alice!")]
    [InlineData("es", "¡Hola, Alice!")]
    [InlineData("fr", "Bonjour, Alice!")]
    [InlineData("de", "Hallo, Alice!")]
    public void Greet_WithSupportedLanguage_ReturnsCorrectGreeting(string lang, string expected)
    {
        var result = _sut.Greet(new GreetingRequest { Name = "Alice", Language = lang }, null!);

        Assert.Equal(expected, result.Message);
        Assert.Equal(lang, result.Language);
    }

    [Fact]
    public void Greet_WithUnknownLanguage_FallsBackToEnglish()
    {
        var result = _sut.Greet(new GreetingRequest { Name = "Bob", Language = "klingon" }, null!);

        Assert.StartsWith("Hello", result.Message);
    }

    [Fact]
    public void Greet_ResponseLength_MatchesActualMessageLength()
    {
        var result = _sut.Greet(new GreetingRequest { Name = "Test", Language = "en" }, null!);

        Assert.Equal(result.Message.Length, result.Length);
    }

    // ── GreetFormal ────────────────────────────────────────────────────────

    [Fact]
    public void GreetFormal_ReturnsEnglishOnly()
    {
        var result = _sut.GreetFormal(new GreetingRequest { Name = "Dr Smith", Language = "fr" }, null!);

        Assert.Equal("en", result.Language);
    }

    [Fact]
    public void GreetFormal_IncludesNameInMessage()
    {
        var result = _sut.GreetFormal(new GreetingRequest { Name = "Dr Smith" }, null!);

        Assert.Contains("Dr Smith", result.Message);
    }

    // ── Transform ──────────────────────────────────────────────────────────

    [Theory]
    [InlineData("upper",   "hello world", "HELLO WORLD")]
    [InlineData("lower",   "HELLO WORLD", "hello world")]
    [InlineData("reverse", "lambit",      "tibmal")]
    [InlineData("shout",   "hello",       "HELLO!!!")]
    public void Transform_WithValidOperation_ReturnsExpectedResult(
        string op, string input, string expected)
    {
        var result = _sut.Transform(new TransformRequest { Text = input, Operation = op }, null!);

        Assert.Equal(expected, result.Transformed);
        Assert.Equal(input, result.Original);
        Assert.Equal(op, result.Operation);
    }

    [Fact]
    public void Transform_WithEmptyText_ThrowsArgumentException()
    {
        Assert.Throws<ArgumentException>(() =>
            _sut.Transform(new TransformRequest { Text = "", Operation = "upper" }, null!));
    }

    [Fact]
    public void Transform_WithUnknownOperation_ThrowsArgumentException()
    {
        Assert.Throws<ArgumentException>(() =>
            _sut.Transform(new TransformRequest { Text = "hello", Operation = "explode" }, null!));
    }

    [Theory]
    [InlineData("upper")]
    [InlineData("lower")]
    [InlineData("reverse")]
    [InlineData("shout")]
    public void Transform_WithAnyValidOperation_PreservesOriginal(string op)
    {
        const string input = "lambit test";
        var result = _sut.Transform(new TransformRequest { Text = input, Operation = op }, null!);

        Assert.Equal(input, result.Original);
    }
}
