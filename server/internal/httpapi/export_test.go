package httpapi

// ParameterProblem exposes parameterProblem to the external tests: no
// operation has parameters yet, so the router cannot trigger it.
var ParameterProblem = parameterProblem

// ServeWithIdleTimeout exposes serve with a configurable idle timeout, so a
// test can prove idle keep-alive connections are closed without waiting for
// the production timeout.
var ServeWithIdleTimeout = serve
