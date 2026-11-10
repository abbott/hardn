package component

// The tests are in package component_test because they take their
// fixtures from pkg/ui/component/fixture, which imports this package.
// These names give them what they test that the package does not export.

// Unexported functions and methods.
var (
	Truncate = Ctx.truncate
	Width    = Ctx.width
	Code     = Ctx.code
	Marker   = marker
	Wrap     = wrap
)

// Unexported words.
const (
	Unknown        = unknown
	ModeLabel      = modeLabel
	ModeLive       = modeLive
	ModePlanOnly   = modePlanOnly
	ModeFromFlag   = modeFromFlag
	ModeFromFile   = modeFromFile
	LockoutRisk    = lockoutRisk
	Unavailable    = unavailable
	ConfirmReverts = confirmReverts
	ConfirmKeep    = confirmKeep
	ConfirmCommand = confirmCommand
)
