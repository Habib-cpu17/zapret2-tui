package ui

import "zapret2/tui/internal/service"

// thin indirection so screens share one place for service actions
var (
	svcStart   = service.Start
	svcStop    = service.Stop
	svcRestart = service.Restart
	svcEnable  = service.SetEnabled
)
