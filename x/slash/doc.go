// Package slash provides a slash command registry for intercepting and
// handling user messages before they enter the LLM inference pipeline.
//
// The Registry implements session.Interceptor and is wired into a
// session.Manager via session.WithInterceptor(). Commands are bound with
// Bind(name, description, handler) and matched when a UserMessageEvent
// content starts with "/<name>". Matched commands are consumed (no LLM
// processing); unmatched text passes through unchanged.
package slash
