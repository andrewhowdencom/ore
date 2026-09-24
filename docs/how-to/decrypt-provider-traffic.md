# Inspect provider HTTP traffic with Wireshark

Go can write the TLS session secrets needed to decrypt a packet capture in
Wireshark. The `stdlib/http` client factory exposes this as
`WithTLSKeyLogWriter`. Ore providers accept that client through their existing
`WithHTTPClient` option.

To try it with the interactive example, start a packet capture on the network
interface used by the application, then run:

```sh
ORE_API_KEY=... ORE_TLS_KEY_LOG_FILE=./tls-secrets.log go run ./examples/tui-chat
```

The example creates the key log with mode `0600` and keeps it open until the
application exits. It overrides `stdlib/http.NewClient`'s short defaults for
internet inference and streaming responses: no total request timeout, a 30
second connect timeout, a 10 second TLS handshake timeout, and a 10 minute
response header timeout. Application context cancellation still stops a turn.

In Wireshark, set **Preferences → Protocols → TLS → (Pre)-Master-Secret log
filename** to the key log file. Inspect the decrypted request and response
streams. For HTTP/2, use the stream ID to distinguish concurrent requests on
one connection. To investigate a cache miss, compare consecutive request
bodies in order (model, instructions, tools, messages), then inspect the
provider's reported cache-token usage in each response.

For another application, construct the client in its composition layer:

```go
client, err := stdlibhttp.NewClient(
	stdlibhttp.WithTimeout(0),
	stdlibhttp.WithResponseHeaderTimeout(10*time.Minute),
	stdlibhttp.WithTLSKeyLogWriter(keyLogFile),
)
if err != nil {
	return err
}
prov, err := openai.New(
	openai.WithAPIKey(apiKey),
	openai.WithHTTPClient(client),
)
```

`WithTracer` independently enables ore's `provider.invoke` and HTTP lifecycle
events. TLS key logging does not put trace/span IDs or retry attempt numbers
in the packet capture, so use timing and request content to correlate them.

The key log and packet capture together reveal **all decrypted traffic on
those connections, including prompts and authorization headers**. Keep both
files private and remove them after the investigation. Go does not enable key
logging from `SSLKEYLOGFILE` by itself.
