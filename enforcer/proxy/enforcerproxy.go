// Package enforcerproxy :: This is the implementation of the RPC client
// It implements the interface of Trireme Enforcer and forwards these
// requests to the actual remote enforcer instead of implementing locally
package enforcerproxy

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/aporeto-inc/trireme-lib/collector"
	"github.com/aporeto-inc/trireme-lib/constants"
	"github.com/aporeto-inc/trireme-lib/enforcer/constants"
	"github.com/aporeto-inc/trireme-lib/enforcer/packetprocessor"
	"github.com/aporeto-inc/trireme-lib/enforcer/policyenforcer"
	"github.com/aporeto-inc/trireme-lib/enforcer/utils/fqconfig"
	"github.com/aporeto-inc/trireme-lib/enforcer/utils/rpcwrapper"
	"github.com/aporeto-inc/trireme-lib/enforcer/utils/secrets"
	"github.com/aporeto-inc/trireme-lib/internal/portset"
	"github.com/aporeto-inc/trireme-lib/internal/processmon"
	"github.com/aporeto-inc/trireme-lib/internal/remoteenforcer"
	"github.com/aporeto-inc/trireme-lib/policy"
	"github.com/aporeto-inc/trireme-lib/utils/crypto"
)

type pkiCertifier interface {
	AuthPEM() []byte
	TransmittedPEM() []byte
	EncodingPEM() []byte
}

type tokenPKICertifier interface {
	TokenPEMs() [][]byte
}

// ProxyInfo is the struct used to hold state about active enforcers in the system
type ProxyInfo struct {
	MutualAuth             bool
	PacketLogs             bool
	Secrets                secrets.Secrets
	serverID               string
	validity               time.Duration
	prochdl                processmon.ProcessManager
	rpchdl                 rpcwrapper.RPCClient
	initDone               map[string]bool
	filterQueue            *fqconfig.FilterQueue
	commandArg             string
	statsServerSecret      string
	procMountPoint         string
	ExternalIPCacheTimeout time.Duration
	portSetInstance        portset.PortSet
	sync.RWMutex
}

// InitRemoteEnforcer method makes a RPC call to the remote enforcer
func (s *ProxyInfo) InitRemoteEnforcer(contextID string) error {

	resp := &rpcwrapper.Response{}
	pkier := s.Secrets.(pkiCertifier)

	request := &rpcwrapper.Request{
		Payload: &rpcwrapper.InitRequestPayload{
			FqConfig:               s.filterQueue,
			MutualAuth:             s.MutualAuth,
			Validity:               s.validity,
			SecretType:             s.Secrets.Type(),
			ServerID:               s.serverID,
			CAPEM:                  pkier.AuthPEM(),
			PublicPEM:              pkier.TransmittedPEM(),
			PrivatePEM:             pkier.EncodingPEM(),
			ExternalIPCacheTimeout: s.ExternalIPCacheTimeout,
			PacketLogs:             s.PacketLogs,
		},
	}

	if s.Secrets.Type() == secrets.PKICompactType {
		payload := request.Payload.(*rpcwrapper.InitRequestPayload)
		payload.Token = s.Secrets.TransmittedKey()
		payload.TokenKeyPEMs = s.Secrets.(tokenPKICertifier).TokenPEMs()
	}

	if err := s.rpchdl.RemoteCall(contextID, remoteenforcer.InitEnforcer, request, resp); err != nil {
		return fmt.Errorf("failed to initialize remote enforcer: status: %s: %s", resp.Status, err)
	}

	s.Lock()
	s.initDone[contextID] = true
	s.Unlock()

	return nil
}

// UpdateSecrets updates the secrets used for signing communication between trireme instances
func (s *ProxyInfo) UpdateSecrets(token secrets.Secrets) error {
	s.Lock()
	defer s.Unlock()
	s.Secrets = token
	return nil
}

// Enforce method makes a RPC call for the remote enforcer enforce method
func (s *ProxyInfo) Enforce(contextID string, puInfo *policy.PUInfo) error {

	err := s.prochdl.LaunchProcess(contextID, puInfo.Runtime.Pid(), puInfo.Runtime.NSPath(), s.rpchdl, s.commandArg, s.statsServerSecret, s.procMountPoint)
	if err != nil {
		return err
	}

	zap.L().Debug("Called enforce and launched process", zap.String("contextID", contextID))

	s.Lock()
	_, ok := s.initDone[contextID]
	s.Unlock()
	if !ok {
		if err = s.InitRemoteEnforcer(contextID); err != nil {
			return err
		}
	}
	pkier := s.Secrets.(pkiCertifier)
	enforcerPayload := &rpcwrapper.EnforcePayload{
		ContextID:        contextID,
		ManagementID:     puInfo.Policy.ManagementID(),
		TriremeAction:    puInfo.Policy.TriremeAction(),
		ApplicationACLs:  puInfo.Policy.ApplicationACLs(),
		NetworkACLs:      puInfo.Policy.NetworkACLs(),
		PolicyIPs:        puInfo.Policy.IPAddresses(),
		Annotations:      puInfo.Policy.Annotations(),
		Identity:         puInfo.Policy.Identity(),
		ReceiverRules:    puInfo.Policy.ReceiverRules(),
		TransmitterRules: puInfo.Policy.TransmitterRules(),
		TriremeNetworks:  puInfo.Policy.TriremeNetworks(),
		ExcludedNetworks: puInfo.Policy.ExcludedNetworks(),
		ProxiedServices:  puInfo.Policy.ProxiedServices(),
		SidecarUID:       puInfo.Policy.SidecarUID(),
	}
	//Only the secrets need to be under lock. They can change async to the enforce call from Updatesecrets
	s.RLock()
	enforcerPayload.CAPEM = pkier.AuthPEM()
	enforcerPayload.PublicPEM = pkier.TransmittedPEM()
	enforcerPayload.PrivatePEM = pkier.EncodingPEM()
	enforcerPayload.SecretType = s.Secrets.Type()
	s.RUnlock()
	request := &rpcwrapper.Request{
		Payload: enforcerPayload,
	}

	err = s.rpchdl.RemoteCall(contextID, remoteenforcer.Enforce, request, &rpcwrapper.Response{})
	if err != nil {
		// We can't talk to the enforcer. Kill it and restart it
		s.Lock()
		delete(s.initDone, contextID)
		s.Unlock()
		s.prochdl.KillProcess(contextID)
		return fmt.Errorf("failed to enforce rules: %s", err)
	}

	return nil
}

// Unenforce stops enforcing policy for the given contextID.
func (s *ProxyInfo) Unenforce(contextID string) error {

	s.Lock()
	delete(s.initDone, contextID)
	s.Unlock()

	return nil
}

// GetFilterQueue returns the current FilterQueueConfig.
func (s *ProxyInfo) GetFilterQueue() *fqconfig.FilterQueue {
	return s.filterQueue
}

// GetPortSetInstance returns nil for the proxy
func (s *ProxyInfo) GetPortSetInstance() portset.PortSet {
	return s.portSetInstance
}

// Start starts the the remote enforcer proxy.
func (s *ProxyInfo) Start() error {
	return nil
}

// Stop stops the remote enforcer.
func (s *ProxyInfo) Stop() error {
	return nil
}

// NewProxyEnforcer creates a new proxy to remote enforcers.
func NewProxyEnforcer(mutualAuth bool,
	filterQueue *fqconfig.FilterQueue,
	collector collector.EventCollector,
	service packetprocessor.PacketProcessor,
	secrets secrets.Secrets,
	serverID string,
	validity time.Duration,
	rpchdl rpcwrapper.RPCClient,
	cmdArg string,
	procMountPoint string,
	ExternalIPCacheTimeout time.Duration,
	packetLogs bool,
) policyenforcer.Enforcer {
	return newProxyEnforcer(
		mutualAuth,
		filterQueue,
		collector,
		service,
		secrets,
		serverID,
		validity,
		rpchdl,
		cmdArg,
		processmon.GetProcessManagerHdl(),
		procMountPoint,
		ExternalIPCacheTimeout,
		nil,
		packetLogs,
	)
}

// newProxyEnforcer creates a new proxy to remote enforcers.
func newProxyEnforcer(mutualAuth bool,
	filterQueue *fqconfig.FilterQueue,
	collector collector.EventCollector,
	service packetprocessor.PacketProcessor,
	secrets secrets.Secrets,
	serverID string,
	validity time.Duration,
	rpchdl rpcwrapper.RPCClient,
	cmdArg string,
	procHdl processmon.ProcessManager,
	procMountPoint string,
	ExternalIPCacheTimeout time.Duration,
	portSetInstance portset.PortSet,
	packetLogs bool,
) policyenforcer.Enforcer {
	statsServersecret, err := crypto.GenerateRandomString(32)

	if err != nil {
		// There is a very small chance of this happening we will log an error here.
		zap.L().Error("Failed to generate random secret for stats reporting.Falling back to static secret", zap.Error(err))
		// We will use current time as the secret
		statsServersecret = time.Now().String()
	}

	proxydata := &ProxyInfo{
		MutualAuth:             mutualAuth,
		Secrets:                secrets,
		serverID:               serverID,
		validity:               validity,
		prochdl:                procHdl,
		rpchdl:                 rpchdl,
		initDone:               make(map[string]bool),
		filterQueue:            filterQueue,
		commandArg:             cmdArg,
		statsServerSecret:      statsServersecret,
		procMountPoint:         procMountPoint,
		ExternalIPCacheTimeout: ExternalIPCacheTimeout,
		PacketLogs:             packetLogs,
		portSetInstance:        portSetInstance,
	}

	zap.L().Debug("Called NewDataPathEnforcer")

	statsServer := rpcwrapper.NewRPCWrapper()
	rpcServer := &StatsServer{rpchdl: statsServer, collector: collector, secret: statsServersecret}

	// Start hte server for statistics collection
	go statsServer.StartServer("unix", rpcwrapper.StatsChannel, rpcServer) // nolint

	return proxydata
}

// NewDefaultProxyEnforcer This is the default datapth method. THis is implemented to keep the interface consistent whether we are local or remote enforcer.
func NewDefaultProxyEnforcer(serverID string,
	collector collector.EventCollector,
	secrets secrets.Secrets,
	rpchdl rpcwrapper.RPCClient,
	procMountPoint string,
) policyenforcer.Enforcer {

	mutualAuthorization := false
	fqConfig := fqconfig.NewFilterQueueWithDefaults()
	defaultExternalIPCacheTimeout, err := time.ParseDuration(enforcerconstants.DefaultExternalIPTimeout)
	if err != nil {
		defaultExternalIPCacheTimeout = time.Second
	}
	defaultPacketLogs := false
	validity := time.Hour * 8760
	return NewProxyEnforcer(
		mutualAuthorization,
		fqConfig,
		collector,
		nil,
		secrets,
		serverID,
		validity,
		rpchdl,
		constants.DefaultRemoteArg,
		procMountPoint,
		defaultExternalIPCacheTimeout,
		defaultPacketLogs,
	)
}

// StatsServer This struct is a receiver for Statsserver and maintains a handle to the RPC StatsServer.
type StatsServer struct {
	collector collector.EventCollector
	rpchdl    rpcwrapper.RPCServer
	secret    string
}

// GetStats is the function called from the remoteenforcer when it has new flow events to publish.
func (r *StatsServer) GetStats(req rpcwrapper.Request, resp *rpcwrapper.Response) error {

	if !r.rpchdl.ProcessMessage(&req, r.secret) {
		zap.L().Error("Message sender cannot be verified")
		return errors.New("message sender cannot be verified")
	}

	payload := req.Payload.(rpcwrapper.StatsPayload)

	for _, record := range payload.Flows {
		r.collector.CollectFlowEvent(record)
	}

	return nil
}
