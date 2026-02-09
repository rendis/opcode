package engine

import (
	"sync"
	"time"

	"github.com/rendis/opcode/pkg/schema"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Failing, rejecting calls
	CircuitHalfOpen                     // Testing recovery
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig configures the circuit breaker behavior.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures before opening the circuit.
	FailureThreshold int
	// Cooldown is how long the circuit stays open before transitioning to half-open.
	Cooldown time.Duration
	// HalfOpenMax is the number of test requests allowed in half-open state.
	HalfOpenMax int
}

// DefaultCircuitBreakerConfig returns a sensible default configuration.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		Cooldown:         30 * time.Second,
		HalfOpenMax:      1,
	}
}

// circuitBreaker tracks failure state for a single action.
type circuitBreaker struct {
	mu                sync.Mutex
	state             CircuitState
	consecutiveFailures int
	lastFailureTime   time.Time
	halfOpenAttempts  int
	config            CircuitBreakerConfig
}

// CircuitBreakerRegistry manages per-action circuit breakers.
type CircuitBreakerRegistry struct {
	mu       sync.Mutex
	breakers map[string]*circuitBreaker
	config   CircuitBreakerConfig
}

// NewCircuitBreakerRegistry creates a new registry with the given config.
func NewCircuitBreakerRegistry(config CircuitBreakerConfig) *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[string]*circuitBreaker),
		config:   config,
	}
}

// AllowRequest checks whether a request to the given action is allowed.
// Returns nil if allowed, or an OpcodeError if the circuit is open.
func (r *CircuitBreakerRegistry) AllowRequest(actionName string) error {
	cb := r.getOrCreate(actionName)
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return nil

	case CircuitOpen:
		// Check if cooldown has elapsed.
		if time.Since(cb.lastFailureTime) >= cb.config.Cooldown {
			cb.state = CircuitHalfOpen
			cb.halfOpenAttempts = 1 // this request counts as the first test request
			return nil
		}
		return schema.NewErrorf(schema.ErrCodeCircuitOpen,
			"circuit breaker open for action %q: %d consecutive failures, cooldown remaining",
			actionName, cb.consecutiveFailures).
			WithDetails(map[string]any{
				"action":               actionName,
				"consecutive_failures": cb.consecutiveFailures,
				"state":                cb.state.String(),
				"cooldown_remaining":   (cb.config.Cooldown - time.Since(cb.lastFailureTime)).String(),
			})

	case CircuitHalfOpen:
		if cb.halfOpenAttempts >= cb.config.HalfOpenMax {
			return schema.NewErrorf(schema.ErrCodeCircuitOpen,
				"circuit breaker half-open for action %q: max test requests reached", actionName)
		}
		cb.halfOpenAttempts++
		return nil
	}

	return nil
}

// RecordSuccess records a successful execution for the action.
func (r *CircuitBreakerRegistry) RecordSuccess(actionName string) {
	cb := r.getOrCreate(actionName)
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures = 0
	cb.halfOpenAttempts = 0
	cb.state = CircuitClosed
}

// RecordFailure records a failed execution for the action.
// Returns the new circuit state.
func (r *CircuitBreakerRegistry) RecordFailure(actionName string) CircuitState {
	cb := r.getOrCreate(actionName)
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures++
	cb.lastFailureTime = time.Now()

	if cb.state == CircuitHalfOpen {
		// Any failure in half-open reopens the circuit.
		cb.state = CircuitOpen
		return CircuitOpen
	}

	if cb.consecutiveFailures >= cb.config.FailureThreshold {
		cb.state = CircuitOpen
		return CircuitOpen
	}

	return cb.state
}

// GetState returns the current state of the circuit for an action.
func (r *CircuitBreakerRegistry) GetState(actionName string) CircuitState {
	cb := r.getOrCreate(actionName)
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check for automatic transition from open to half-open.
	if cb.state == CircuitOpen && time.Since(cb.lastFailureTime) >= cb.config.Cooldown {
		cb.state = CircuitHalfOpen
		cb.halfOpenAttempts = 0
	}

	return cb.state
}

// GetStats returns diagnostic information about a circuit breaker.
func (r *CircuitBreakerRegistry) GetStats(actionName string) map[string]any {
	cb := r.getOrCreate(actionName)
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return map[string]any{
		"action":               actionName,
		"state":                cb.state.String(),
		"consecutive_failures": cb.consecutiveFailures,
		"failure_threshold":    cb.config.FailureThreshold,
		"cooldown":             cb.config.Cooldown.String(),
	}
}

func (r *CircuitBreakerRegistry) getOrCreate(actionName string) *circuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()
	cb, ok := r.breakers[actionName]
	if !ok {
		cb = &circuitBreaker{
			state:  CircuitClosed,
			config: r.config,
		}
		r.breakers[actionName] = cb
	}
	return cb
}
