package tui

import "testing"

func fillInputs(m *Model, vals [6]string) {
	for i := range m.inputs {
		m.inputs[i].SetValue(vals[i])
	}
	m.applyForm()
}

func TestValidateFormOK(t *testing.T) {
	m := New()
	fillInputs(&m, [6]string{"vpn.example.com", "203.0.113.10", "443", "alice", "S3cure!!-pass", "admin@example.com"})
	if !m.validateForm() {
		t.Fatalf("expected valid, errs=%v msg=%s", m.fieldErrs, m.errMsg)
	}
}

func TestValidateFormMarksFields(t *testing.T) {
	m := New()
	fillInputs(&m, [6]string{"not a domain", "999.1.1.1", "99999", "", "short", "no-at-sign"})
	if m.validateForm() {
		t.Fatal("expected invalid")
	}
	for i, want := range map[int]bool{0: true, 1: true, 2: true, 3: true, 4: true, 5: true} {
		if want && m.fieldErrs[i] == "" {
			t.Errorf("field %d should carry an error", i)
		}
	}
}

func TestValidateFormIPMode(t *testing.T) {
	m := New()
	m.cfg.Mode = "ip"
	fillInputs(&m, [6]string{"203.0.113.10", "", "8080", "bob", "long-enough-pass", ""})
	if !m.validateForm() {
		t.Fatalf("expected valid IP mode, errs=%v", m.fieldErrs)
	}
}
