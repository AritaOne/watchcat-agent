package collector

import "runtime"

func runtimeVersion() string { return runtime.Version() }
