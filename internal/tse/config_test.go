package tse

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gpu/urna-watch/internal/domain"
)

func testConfig(t *testing.T) *wireConfig {
	t.Helper()
	var cfg wireConfig
	if err := json.Unmarshal(fixture(t, "ele-c.json"), &cfg); err != nil {
		t.Fatalf("config fixture: %v", err)
	}
	return &cfg
}

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	a, err := NewAdapter(Options{BaseURL: "https://example.test", Environment: "oficial"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func ref(election, contest, uf string) domain.ElectionRef {
	scope := domain.Scope{Kind: domain.ScopeState, UF: uf}
	if uf == "br" {
		scope = domain.Scope{Kind: domain.ScopeBrazil, UF: "br"}
	}
	return domain.ElectionRef{ElectionCode: election, ContestCode: contest, Scope: scope}
}

func TestResolveOfficialEndpoints(t *testing.T) {
	a := testAdapter(t)
	cfg := testConfig(t)
	cases := []struct {
		name string
		ref  domain.ElectionRef
		want string
	}{
		{
			name: "federal runoff brazil",
			ref:  ref("6258", "1", "br"),
			want: "https://example.test/oficial/ele2026/6258/dados/br/br-c0001-e006258-u.json",
		},
		{
			name: "federal first round per uf",
			ref:  ref("6257", "1", "sp"),
			want: "https://example.test/oficial/ele2026/6257/dados/sp/sp-c0001-e006257-u.json",
		},
		{
			name: "state first round per uf",
			ref:  ref("6259", "3", "pi"),
			want: "https://example.test/oficial/ele2026/6259/dados/pi/pi-c0003-e006259-u.json",
		},
		{
			name: "state runoff per uf",
			ref:  ref("6260", "3", "am"),
			want: "https://example.test/oficial/ele2026/6260/dados/am/am-c0003-e006260-u.json",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, got, err := a.resolveElection(cfg, tc.ref)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != tc.want {
				t.Fatalf("url = %q, want %q", got, tc.want)
			}
			if info.ContestCode != tc.ref.ContestCode || info.ElectionCode != tc.ref.ElectionCode {
				t.Fatalf("info = %+v", info)
			}
			if info.SourcePage == "" {
				t.Fatal("source page empty")
			}
		})
	}
}

func TestResolveRejectsInvalidSelections(t *testing.T) {
	a := testAdapter(t)
	cfg := testConfig(t)
	cases := []struct {
		name string
		ref  domain.ElectionRef
		want string
	}{
		{"governor brazil-wide", ref("6260", "3", "br"), "informe uma UF"},
		{"governor outside runoff", ref("6260", "3", "sp"), "não existe"},
		{"wrong round", domain.ElectionRef{ElectionCode: "6258", ContestCode: "1", Round: 1, Scope: domain.Scope{Kind: domain.ScopeBrazil, UF: "br"}}, "turno"},
		{"unknown election", ref("9999", "1", "br"), "não encontrada"},
		{"unknown contest", ref("6258", "99", "br"), "não existe"},
		{"state election brazil-wide", ref("6259", "3", "br"), "informe uma UF"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := a.resolveElection(cfg, tc.ref)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestResolveMunicipality(t *testing.T) {
	a := testAdapter(t)
	cfg := testConfig(t)
	r := domain.ElectionRef{
		ElectionCode: "6284",
		ContestCode:  "11",
		Scope:        domain.Scope{Kind: domain.ScopeMunicipality, UF: "sp", Municipality: "67458"},
	}
	info, got, err := a.resolveElection(cfg, r)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "https://example.test/oficial/ele2026/6284/dados/sp/sp67458-c0011-e006284-u.json"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
	if !strings.Contains(info.ScopeName, "67458") {
		t.Fatalf("scope name = %q", info.ScopeName)
	}
}

func TestResolveMunicipalityRequiresUF(t *testing.T) {
	a := testAdapter(t)
	cfg := testConfig(t)
	r := domain.ElectionRef{
		ElectionCode: "6284",
		ContestCode:  "11",
		Scope:        domain.Scope{Kind: domain.ScopeMunicipality, UF: "br", Municipality: "67458"},
	}
	if _, _, err := a.resolveElection(cfg, r); err == nil {
		t.Fatal("expected error")
	}
}
