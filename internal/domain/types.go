// Package domain holds the pure types of the Nous coordination layer.
//
// Layering rule: this package has no I/O, no logging, no SQL, no
// transport. It models commitments, decisions, interventions, goals,
// and tasks as data with validation and transition rules. Anything
// that touches Postgres, gRPC, or an LLM lives in an outer package.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// ID aliases keep call sites readable without sacrificing the
// uuid.UUID interop the rest of the stack already speaks.
type (
	GoalID         = uuid.UUID
	CommitmentID   = uuid.UUID
	TaskID         = uuid.UUID
	PlanID         = uuid.UUID
	DecisionID     = uuid.UUID
	InterventionID = uuid.UUID
	AssignmentID   = uuid.UUID
	AgentID        = string
)

// Priority orders work within a backlog. Higher = more urgent.
// The numeric values are stable so they can be persisted as INT and
// compared in SQL ORDER BY without a translation table.
type Priority int

const (
	PriorityLow      Priority = 0
	PriorityNormal   Priority = 50
	PriorityHigh     Priority = 75
	PriorityUrgent   Priority = 100
	PriorityCritical Priority = 150
)

// SourceRef points back at the evidence that produced a domain object.
// Mnemos events, Chronos signals, raw transcripts — all live behind
// this single struct. The Locator is opaque to Nous (a URI, a UUID
// string, an event ID) and is only re-interpreted by the producing
// system on read-back.
type SourceRef struct {
	Kind    SourceKind `json:"kind"`
	Locator string     `json:"locator"`
}

// SourceKind enumerates the systems Nous recognises as evidence
// origins. New kinds should be added here, not as ad-hoc strings, so
// repository projections stay typed.
type SourceKind string

const (
	SourceMnemosEvent   SourceKind = "mnemos.event"
	SourceMnemosMemory  SourceKind = "mnemos.memory"
	SourceChronosSignal SourceKind = "chronos.signal"
	SourceUserInput     SourceKind = "user.input"
	SourcePraxisResult  SourceKind = "praxis.result"
	SourceNousDecision  SourceKind = "nous.decision"
)

// EntityRef references a real-world thing a commitment or task is
// about — a person, project, document, tool. Kept symbolic so Nous
// does not own the entity model; that lives in Mnemos.
type EntityRef struct {
	Kind EntityKind `json:"kind"`
	Name string     `json:"name"`
	// ID is optional: when Mnemos has resolved the entity, the
	// canonical identifier travels with the reference so downstream
	// systems can rehydrate without a name lookup.
	ID string `json:"id,omitempty"`
}

// EntityKind enumerates entity flavours. Open-ended on purpose;
// "other" is acceptable for things Mnemos has not classified yet.
type EntityKind string

const (
	EntityPerson   EntityKind = "person"
	EntityProject  EntityKind = "project"
	EntityTool     EntityKind = "tool"
	EntityDocument EntityKind = "document"
	EntityTeam     EntityKind = "team"
	EntityOther    EntityKind = "other"
)

// MemoryRef identifies a Mnemos memory or claim. Nous holds these as
// pointers — it does not duplicate Mnemos content.
type MemoryRef struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ChronosSignal is the lean projection of a Chronos signal Nous needs
// for risk evaluation. The full record stays in Chronos; this struct
// is a transport-safe copy carried into RiskEngine inputs.
type ChronosSignal struct {
	ID         uuid.UUID   `json:"id"`
	Pattern    string      `json:"pattern"`
	ScopeID    uuid.UUID   `json:"scope_id"`
	Series     uuid.UUID   `json:"series"`
	Confidence float64     `json:"confidence"`
	DetectedAt time.Time   `json:"detected_at"`
	Entities   []EntityRef `json:"entities,omitempty"`
}
