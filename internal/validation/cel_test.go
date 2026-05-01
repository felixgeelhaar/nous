package validation

import (
	"strings"
	"testing"
)

func TestValidator_NoRulesAlwaysPasses(t *testing.T) {
	t.Parallel()
	v, err := NewValidator(nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := v.Validate(Input{Text: "anything"}); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestValidator_TextLengthRule(t *testing.T) {
	t.Parallel()
	v, err := NewValidator([]Rule{
		{Name: "max_text_length", Expr: "text_len <= 100"},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := v.Validate(Input{Text: "short"}); err != nil {
		t.Errorf("short text rejected: %v", err)
	}
	long := strings.Repeat("x", 200)
	if err := v.Validate(Input{Text: long}); err == nil {
		t.Error("long text accepted")
	} else if !IsRuleViolation(err) {
		t.Errorf("err = %v (want RuleViolation)", err)
	}
}

func TestValidator_OwnerIDFormatRule(t *testing.T) {
	t.Parallel()
	v, err := NewValidator([]Rule{
		{Name: "owner_id_prefix", Expr: `owner_id.startsWith("u_")`},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := v.Validate(Input{OwnerID: "u_alice"}); err != nil {
		t.Errorf("rejected valid owner: %v", err)
	}
	if err := v.Validate(Input{OwnerID: "alice"}); err == nil {
		t.Error("invalid owner accepted")
	}
}

func TestValidator_SourceKindWhitelist(t *testing.T) {
	t.Parallel()
	v, err := NewValidator([]Rule{
		{Name: "source_kinds_whitelist", Expr: `source_kinds.all(k, k in ["mnemos", "chronos", "manual"])`},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := v.Validate(Input{SourceKinds: []string{"mnemos", "manual"}}); err != nil {
		t.Errorf("rejected valid kinds: %v", err)
	}
	if err := v.Validate(Input{SourceKinds: []string{"unknown"}}); err == nil {
		t.Error("unknown kind accepted")
	}
}

func TestValidator_RejectsMalformedExpression(t *testing.T) {
	t.Parallel()
	_, err := NewValidator([]Rule{{Name: "bad", Expr: "this is not valid CEL"}})
	if err == nil {
		t.Fatal("want compile error")
	}
}

func TestValidator_RejectsNonBoolExpression(t *testing.T) {
	t.Parallel()
	_, err := NewValidator([]Rule{{Name: "wrong_type", Expr: `text + "x"`}})
	if err == nil {
		t.Fatal("want type error")
	}
}

func TestValidator_FirstFailingRuleReported(t *testing.T) {
	t.Parallel()
	v, err := NewValidator([]Rule{
		{Name: "first", Expr: "text_len > 0"},
		{Name: "second", Expr: "text_len < 100"},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	verr := v.Validate(Input{Text: ""})
	if verr == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(verr.Error(), `"first"`) {
		t.Errorf("err = %v (want first rule)", verr)
	}
}
