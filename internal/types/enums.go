package types

type JobIssue string

const (
	// Tyres & Wheels
	FlatTyres           JobIssue = "flat_tyres"
	TyreBurst           JobIssue = "tyre_burst"
	WheelAlignmentIssue JobIssue = "wheel_alignment_issue"

	// Battery & Starting
	BatteryProblem    JobIssue = "battery_problem"
	DeadBattery       JobIssue = "dead_battery"
	AlternatorFailure JobIssue = "alternator_failure"
	StarterMotorFault JobIssue = "starter_motor_fault"

	// Engine Issues
	EngineOverheating JobIssue = "engine_overheating"
	EngineKnocking    JobIssue = "engine_knocking"
	EngineMisfire     JobIssue = "engine_misfire"
	EngineStalling    JobIssue = "engine_stalling"

	// Braking System
	BrakeFailure   JobIssue = "brake_failure"
	BrakePadWorn   JobIssue = "brake_pad_worn"
	BrakeFluidLeak JobIssue = "brake_fluid_leak"
	ABSFault       JobIssue = "abs_fault"

	// Transmission
	GearNotShifting  JobIssue = "gear_not_shifting"
	ClutchProblem    JobIssue = "clutch_problem"
	TransmissionLeak JobIssue = "transmission_leak"

	// Electrical
	ElectricalFault       JobIssue = "electrical_fault"
	HeadlightIssue        JobIssue = "headlight_issue"
	DashboardWarningLight JobIssue = "dashboard_warning_light"
	WiringProblem         JobIssue = "wiring_problem"
	FuseProblem           JobIssue = "fuse_problem"

	// Fluids & Leakage
	OilLeak           JobIssue = "oil_leak"
	CoolantLeak       JobIssue = "coolant_leak"
	FuelLeak          JobIssue = "fuel_leak"
	PowerSteeringLeak JobIssue = "power_steering_leak"

	// Steering & Suspension
	SteeringProblem    JobIssue = "steering_problem"
	SuspensionNoise    JobIssue = "suspension_noise"
	ShockAbsorberIssue JobIssue = "shock_absorber_issue"

	// Fuel System
	FuelPumpFailure    JobIssue = "fuel_pump_failure"
	InjectorProblem    JobIssue = "injector_problem"
	CarNotAccelerating JobIssue = "car_not_accelerating"

	// AC & Comfort
	ACNotCooling JobIssue = "ac_not_cooling"

	// Lock & Key
	KeyLockedInside JobIssue = "key_locked_inside"
	IgnitionProblem JobIssue = "ignition_problem"

	// Emergency / Misc
	CarAccidentDamage JobIssue = "car_accident_damage"
	TowingNeeded      JobIssue = "towing_needed"
	GeneralInspection JobIssue = "general_inspection"
	Other             JobIssue = "other"
)

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusAccepted   JobStatus = "accepted"
	JobStatusEnRoute    JobStatus = "en_route"
	JobStatusArrived    JobStatus = "arrived"
	JobStatusInProgress JobStatus = "in_progress"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusCancelled  JobStatus = "cancelled"
	JobStatusRejected   JobStatus = "rejected"
	JobStatusDisputed   JobStatus = "disputed"
)
