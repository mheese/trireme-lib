package trireme

import (
	"github.com/aporeto-inc/trireme-lib/constants"
	"github.com/aporeto-inc/trireme-lib/enforcer/utils/secrets"
	"github.com/aporeto-inc/trireme-lib/internal/supervisor"
	"github.com/aporeto-inc/trireme-lib/policy"
	"github.com/aporeto-inc/trireme-lib/rpc/events"
)

// Trireme is the main interface to the Trireme package.
type Trireme interface {

	// PURuntime returns a getter for a specific contextID.
	PURuntime(contextID string) (policy.RuntimeReader, error)

	// Start starts the component.
	Start() error

	// Stop stops the component.
	Stop() error

	// Supervisor returns the supervisor for a given PU type
	Supervisor(kind constants.PUType) supervisor.Supervisor

	// processor.ProcessingUnitsHandler
	// CreatePURuntime is called when a monitor detects creation of a new ProcessingUnit.
	CreatePURuntime(contextID string, runtimeInfo *policy.PURuntime) error

	// HandlePUEvent is called by all monitors when a PU event is generated. The implementer
	// is responsible to update all components by explicitly adding a new PU.
	HandlePUEvent(contextID string, event events.Event) error

	// PolicyUpdater
	// UpdatePolicy updates the policy of the isolator for a container.
	UpdatePolicy(contextID string, policy *policy.PUPolicy) error

	// SecretsUpdater
	// UpdateSecrets updates the secrets of running enforcers managed by trireme. Remote enforcers will get the secret updates with the next policy push
	UpdateSecrets(secrets secrets.Secrets) error
}

// A PolicyUpdater has the ability to receive an update for a specific policy.
type PolicyUpdater interface {

	// UpdatePolicy updates the policy of the isolator for a container.
	UpdatePolicy(contextID string, policy *policy.PUPolicy) error
}

// A PolicyResolver is responsible of creating the Policies for a specific Processing Unit.
// The PolicyResolver also got the ability to update an already instantiated policy.
type PolicyResolver interface {

	// ResolvePolicy returns the policy.PUPolicy associated with the given contextID using the given policy.RuntimeReader.
	ResolvePolicy(contextID string, RuntimeReader policy.RuntimeReader) (*policy.PUPolicy, error)

	// HandleDeletePU is called when a PU is stopped/killed.
	HandlePUEvent(contextID string, eventType events.Event)
}

// SecretsUpdater provides an interface to update the secrets of enforcers managed by trireme at runtime
type SecretsUpdater interface {
	// UpdateSecrets updates the secrets of running enforcers managed by trireme. Remote enforcers will get the secret updates with the next policy push
	UpdateSecrets(secrets secrets.Secrets) error
}
