# dotnet-greeting — build and publish helpers for lambit

.PHONY: build publish test clean

build:
	dotnet build src/GreetingFunction/ --nologo -v q

# publish is required before lambit can invoke the handlers.
# lambit's [i] Invoke will trigger 'dotnet build' via its BuildArgs — but for
# the first run you should publish once so the shim can find all dependencies.
publish: build
	dotnet publish src/GreetingFunction/ --nologo -v q -o .lambit/lambda-out
	@echo "Published to .lambit/lambda-out — lambit is now ready to invoke."

test:
	dotnet test tests/GreetingFunction.Tests/ --nologo -v q

clean:
	rm -rf src/GreetingFunction/bin src/GreetingFunction/obj
	rm -rf tests/GreetingFunction.Tests/bin tests/GreetingFunction.Tests/obj
	rm -rf .lambit/lambda-out .lambit/dotnet-runner/bin .lambit/dotnet-runner/obj
