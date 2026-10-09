// Package agent runs harnesses: it is Agenty's own agent loop, written on the
// provider-neutral model interface rather than on an agent framework, so
// that governance sits inside the tool-call path and the harness definition
// stays framework-free.
//
// # Role in the architecture
//
// The server (internal/server) is the only caller outside tests. For each
// run a worker claims, it builds an Agent with New, from the harness version
// the run's conversation is pinned to, the model that package server's
// NewModel builds for it (internal/model/anthropic in production), the MCP
// servers its conversation leases, central policy from the operator config,
// an audit log and a transcript that both write to the store. It then calls
// Continue for a new run or a follow-up, or Resume for a run whose call
// waited for approval and has since been answered, and finally Close.
//
// The package calls down into:
//
//   - internal/harness for the definition it runs, validated again in New;
//   - internal/policy, which compiles central and harness policy as separate
//     layers;
//   - internal/toolgateway, which owns the granted tools and their servers
//     and decides, executes and records every tool call;
//   - internal/model, through which every model call goes.
//
// # Contents
//
//   - Config and New: the wiring of one run, and its validation. New compiles
//     policy and builds the run's own Gateway, which starts the tool servers
//     the grants need.
//   - Agent: one run. Continue starts it on an input; Resume goes on with it
//     after an approval; both end in loop, the step loop shared by the two.
//   - Transcript: the interface through which every message of the run is
//     recorded as it joins the conversation.
//   - Suspended and Resumption: the error a run stops with at a call that
//     waits for approval, and what Resume needs to go on from that call.
//   - PromptDigest: an identifier of what the run sends the model before the
//     conversation, which the server stores to decide whether a later run
//     may replay a reply in the provider's own form.
//
// # How a run flows
//
// Each step is one model call. The loop sends the instructions, the earlier
// runs' conversation (history), the run's own messages and the gateway's
// tool definitions. A reply without tool calls is the answer and ends the
// run. A reply with calls has them run in order through the gateway: a
// denial or a tool error becomes an error result the model sees and can
// react to, while an audit failure or cancellation ends the run before any
// further call. The results go back to the model as the next user message,
// and the loop takes the next step. On the last step the harness allows, the
// calls of a reply are not run: they are recorded as not run, so the
// transcript ends where a follow-up can continue, and the run returns
// ErrMaxSteps.
//
// A call that policy marks as requiring approval stops the run with a
// *Suspended error, holding the index of the call in its reply and the
// results of the calls before it. The server stores these and lets the run
// wait without a worker. Once the call is answered, a new Agent is built for
// the same run ID, with the run's audit log as Config.Records so that its
// call counts carry over, and Resume decides the waiting call again through
// the gateway, runs the calls after it and continues the loop, counting the
// steps the run took before it stopped.
//
// History is replayed as stored. When the gateway offers no tools, as a
// harness version grants none, earlier tool calls and results are written
// as text, as providers refuse tool calls in a conversation without tools.
//
// # Trust-model guarantees
//
//   - 1, the model is not trusted: what the model asks for is only ever an
//     input to the gateway's deterministic checks, and a refused call is
//     reported back to the model, never executed.
//   - 2, every side effect goes through the gateway: the agent never calls a
//     tool itself; each call goes through Gateway.Call or Gateway.Resume.
//   - 3 and 4, default deny and strictest wins: the agent hands the gateway
//     only the harness grants and compiles central and harness policy as
//     separate layers, so harness policy can only tighten central policy.
//   - 5, credentials never reach the model: the gateway redacts tool output
//     and errors with Config.Redactor before the agent puts them into the
//     conversation.
//   - 6, every tool call is recorded: an audit failure ends the run instead
//     of letting it go on with an unrecorded call.
package agent
