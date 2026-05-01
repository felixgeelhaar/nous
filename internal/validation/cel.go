// Package validation provides CEL (Common Expression Language)
// based input validation for the Nous extract endpoint and any
// future write paths that need configurable per-tenant rules.
//
// CEL is the right tool here because the rules are operator-defined
// — different tenants want different policies (max text length,
// banned source kinds, owner_id format) without redeploying Nous.
// The expressions compile once at startup; runtime evaluation is a
// single map lookup plus interpreter pass.
package validation

import (
	"errors"
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Rule is one named expression. Name is shown in error messages so
// operators can identify which rule rejected a request.
type Rule struct {
	Name string
	Expr string
}

// Validator evaluates a fixed set of CEL rules against extract-style
// inputs. Construct once at startup with [NewValidator]; safe for
// concurrent use after construction.
type Validator struct {
	env      *cel.Env
	programs []namedProgram
}

type namedProgram struct {
	name string
	prog cel.Program
}

// Input is the CEL evaluation context. Keys: `owner_id` (string),
// `text` (string), `text_len` (int), `source_kinds` (list<string>).
type Input struct {
	OwnerID     string
	Text        string
	SourceKinds []string
}

// NewValidator compiles rules. An empty rule slice produces a
// validator that always passes.
func NewValidator(rules []Rule) (*Validator, error) {
	env, err := cel.NewEnv(
		cel.Variable("owner_id", cel.StringType),
		cel.Variable("text", cel.StringType),
		cel.Variable("text_len", cel.IntType),
		cel.Variable("source_kinds", cel.ListType(cel.StringType)),
	)
	if err != nil {
		return nil, fmt.Errorf("validation: env: %w", err)
	}
	progs := make([]namedProgram, 0, len(rules))
	for _, r := range rules {
		ast, iss := env.Compile(r.Expr)
		if iss != nil && iss.Err() != nil {
			return nil, fmt.Errorf("validation: rule %q: %w", r.Name, iss.Err())
		}
		// Rules must produce a bool.
		if !ast.OutputType().IsExactType(cel.BoolType) {
			return nil, fmt.Errorf("validation: rule %q must return bool, got %s", r.Name, ast.OutputType())
		}
		prog, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("validation: rule %q program: %w", r.Name, err)
		}
		progs = append(progs, namedProgram{name: r.Name, prog: prog})
	}
	return &Validator{env: env, programs: progs}, nil
}

// Validate evaluates every rule. The first rule that returns false
// produces a [RuleViolation] error; rules that error out at runtime
// produce a wrapped CEL error so operators can fix expressions.
// Returns nil when every rule passes (or the rule set is empty).
func (v *Validator) Validate(in Input) error {
	if v == nil || len(v.programs) == 0 {
		return nil
	}
	vars := map[string]any{
		"owner_id":     in.OwnerID,
		"text":         in.Text,
		"text_len":     int64(len(in.Text)),
		"source_kinds": in.SourceKinds,
	}
	for _, p := range v.programs {
		out, _, err := p.prog.Eval(vars)
		if err != nil {
			return fmt.Errorf("validation: rule %q eval: %w", p.name, err)
		}
		if out == types.False || refIsFalse(out) {
			return &RuleViolation{Rule: p.name}
		}
	}
	return nil
}

// RuleViolation is returned when a rule's expression evaluates to false.
type RuleViolation struct {
	Rule string
}

func (e *RuleViolation) Error() string {
	return fmt.Sprintf("validation: rule %q rejected the request", e.Rule)
}

// IsRuleViolation reports whether err is a CEL rule rejection (vs a
// programmer error like a malformed expression).
func IsRuleViolation(err error) bool {
	var rv *RuleViolation
	return errors.As(err, &rv)
}

func refIsFalse(v ref.Val) bool {
	if b, ok := v.Value().(bool); ok {
		return !b
	}
	return false
}
