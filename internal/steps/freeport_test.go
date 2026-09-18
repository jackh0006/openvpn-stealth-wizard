package steps

import (
	"context"
	"testing"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/logx"
)

func TestFreePortRefusesSSH(t *testing.T) {
	if err := FreePort(context.Background(), 22, logx.New()); err == nil {
		t.Fatal("port 22 must never be freed")
	}
}

func TestPortOwnerShape(t *testing.T) {
	o := PortOwner(9)
	_ = o.String()
}
