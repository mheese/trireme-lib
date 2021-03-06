package uidmonitor

import (
	"fmt"
	"regexp"

	"github.com/aporeto-inc/trireme-lib/constants"
	"github.com/aporeto-inc/trireme-lib/internal/monitor/instance"
	"github.com/aporeto-inc/trireme-lib/internal/monitor/rpc/registerer"
	"github.com/aporeto-inc/trireme-lib/rpc/events"
	"github.com/aporeto-inc/trireme-lib/rpc/processor"
	"github.com/aporeto-inc/trireme-lib/utils/cache"
	"github.com/aporeto-inc/trireme-lib/utils/cgnetcls"
	"github.com/aporeto-inc/trireme-lib/utils/contextstore"
)

// Config is the configuration options to start a CNI monitor
type Config struct {
	EventMetadataExtractor events.EventMetadataExtractor
	StoredPath             string
	ReleasePath            string
}

// DefaultConfig provides default configuration for uid monitor
func DefaultConfig() *Config {

	return &Config{
		EventMetadataExtractor: events.UIDMetadataExtractor,
		StoredPath:             "/var/run/trireme/uid",
		ReleasePath:            "/var/lib/aporeto/cleaner",
	}
}

// SetupDefaultConfig adds defaults to a partial configuration
func SetupDefaultConfig(uidConfig *Config) *Config {

	defaultConfig := DefaultConfig()

	if uidConfig.ReleasePath == "" {
		uidConfig.ReleasePath = defaultConfig.ReleasePath
	}
	if uidConfig.StoredPath == "" {
		uidConfig.StoredPath = defaultConfig.StoredPath
	}
	if uidConfig.EventMetadataExtractor == nil {
		uidConfig.EventMetadataExtractor = defaultConfig.EventMetadataExtractor
	}

	return uidConfig
}

// uidMonitor captures all the monitor processor information for a UIDLoginPU
// It implements the EventProcessor interface of the rpc monitor
type uidMonitor struct {
	proc *uidProcessor
}

// New returns a new implmentation of a monitor implmentation
func New() monitorinstance.Implementation {

	return &uidMonitor{
		proc: &uidProcessor{},
	}
}

// Start implements Implementation interface
func (u *uidMonitor) Start() error {

	if err := u.proc.config.IsComplete(); err != nil {
		return fmt.Errorf("uid: %s", err)
	}

	if err := u.ReSync(); err != nil {
		return err
	}

	return nil
}

// Stop implements Implementation interface
func (u *uidMonitor) Stop() error {

	return nil
}

// SetupConfig provides a configuration to implmentations. Every implmentation
// can have its own config type.
func (u *uidMonitor) SetupConfig(registerer registerer.Registerer, cfg interface{}) error {

	defaultConfig := DefaultConfig()
	if cfg == nil {
		cfg = defaultConfig
	}

	uidConfig, ok := cfg.(*Config)
	if !ok {
		return fmt.Errorf("Invalid configuration specified")
	}

	if registerer != nil {
		if err := registerer.RegisterProcessor(constants.UIDLoginPU, u.proc); err != nil {
			return err
		}
	}

	// Setup defaults
	uidConfig = SetupDefaultConfig(uidConfig)

	// Setup config
	u.proc.netcls = cgnetcls.NewCgroupNetController(uidConfig.ReleasePath)
	u.proc.contextStore = contextstore.NewFileContextStore(uidConfig.StoredPath, u.proc.RemapData)
	u.proc.storePath = uidConfig.StoredPath
	u.proc.regStart = regexp.MustCompile("^[a-zA-Z0-9_].{0,11}$")
	u.proc.regStop = regexp.MustCompile("^/trireme/[a-zA-Z0-9_].{0,11}$")
	u.proc.putoPidMap = cache.NewCache("putoPidMap")
	u.proc.pidToPU = cache.NewCache("pidToPU")
	u.proc.metadataExtractor = uidConfig.EventMetadataExtractor
	if u.proc.metadataExtractor == nil {
		return fmt.Errorf("Unable to setup a metadata extractor")
	}

	return nil
}

// SetupHandlers sets up handlers for monitors to invoke for various events such as
// processing unit events and synchronization events. This will be called before Start()
// by the consumer of the monitor
func (u *uidMonitor) SetupHandlers(m *processor.Config) {

	u.proc.config = m
}

func (u *uidMonitor) ReSync() error {

	return u.proc.ReSync(nil)
}
