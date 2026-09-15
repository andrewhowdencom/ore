# Use a ChatGPT subscription with an ore agent

The experimental `x/provider/codex` package lets an ore application call the
Codex Responses service using a ChatGPT Plus or Pro subscription. It does not
run Codex App Server, and it does not read `~/.codex/auth.json`.

Use `x/provider/openai` instead when you want the documented OpenAI Platform
API and API-based billing.

## Device-code login

Construct the provider, start login, render the URL and code in your own UI,
and wait for completion:

```go
p, err := codex.New()
if err != nil {
	return err
}

login, err := p.StartDeviceLogin(ctx)
if err != nil {
	return err
}

fmt.Printf("Open %s and enter %s\n", login.VerificationURL, login.UserCode)
if err := login.Wait(ctx); err != nil {
	return err
}
```

For a desktop application, call `StartBrowserLogin` instead and open its
`VerificationURL`. The provider runs a temporary loopback callback listener;
`Wait` reports success, OAuth failure, or cancellation. Call `login.Cancel()`
when the application abandons the flow.

After login, use `p` anywhere a `provider.Provider` is accepted. Model identity
still comes from the `models.Spec` supplied to the ore loop.

## Credentials and logout

Credentials are stored separately from Codex CLI under the user's configuration
directory in `ore/codex-credentials.json`. The directory and file are restricted
to the current user. The provider refreshes expiring tokens automatically and
coalesces concurrent refresh attempts.

Call `p.Logout(ctx)` to make a best-effort token-revocation request and remove
the local credential file. Neither the login API nor the provider API returns
access or refresh tokens to the application.

The ChatGPT Codex service is a compatibility surface rather than a documented
general-purpose OpenAI Platform API. Keep the integration pinned and tested;
upstream protocol or OAuth details may change.
