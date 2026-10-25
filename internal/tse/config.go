package tse

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/format"
)

// wireConfig is the subset of /comum/config/ele-c.json that this project uses.
// Field names follow the official EA20 files.
type wireConfig struct {
	DG  string       `json:"dg"`
	HG  string       `json:"hg"`
	IDG string       `json:"idg"`
	F   string       `json:"f"`
	ARQ []wireArq    `json:"arq"`
	PL  []wirePleito `json:"pl"`
}

type wireArq struct {
	TP  string `json:"tp"`
	Dir string `json:"dir"`
}

type wirePleito struct {
	CD   string         `json:"cd"`
	CDPR string         `json:"cdpr"`
	C    string         `json:"c"`
	DT   string         `json:"dt"`
	E    []wireElection `json:"e"`
}

type wireElection struct {
	CD   string      `json:"cd"`
	CDT2 string      `json:"cdt2"`
	NM   string      `json:"nm"`
	T    string      `json:"t"`
	TP   string      `json:"tp"`
	ABR  []wireScope `json:"abr"`
}

type wireScope struct {
	CD string        `json:"cd"`
	CP []wireContest `json:"cp"`
}

type wireContest struct {
	CD string `json:"cd"`
	DS string `json:"ds"`
}

type wireMunicipalities struct {
	ABR []struct {
		CD string `json:"cd"`
		DS string `json:"ds"`
		MU []struct {
			CD string `json:"cd"`
			NM string `json:"nm"`
		} `json:"mu"`
	} `json:"abr"`
}

var stateNames = map[string]string{
	"ac": "Acre", "al": "Alagoas", "ap": "Amapá", "am": "Amazonas",
	"ba": "Bahia", "ce": "Ceará", "df": "Distrito Federal", "es": "Espírito Santo",
	"go": "Goiás", "ma": "Maranhão", "mt": "Mato Grosso", "ms": "Mato Grosso do Sul",
	"mg": "Minas Gerais", "pa": "Pará", "pb": "Paraíba", "pr": "Paraná",
	"pe": "Pernambuco", "pi": "Piauí", "rj": "Rio de Janeiro", "rn": "Rio Grande do Norte",
	"rs": "Rio Grande do Sul", "ro": "Rondônia", "rr": "Roraima", "sc": "Santa Catarina",
	"sp": "São Paulo", "se": "Sergipe", "to": "Tocantins",
}

// StateName returns the official state name for a UF code, or the uppercase
// code itself when unknown.
func StateName(uf string) string {
	if n, ok := stateNames[strings.ToLower(strings.TrimSpace(uf))]; ok {
		return n
	}
	return strings.ToUpper(strings.TrimSpace(uf))
}

func scopeName(ref domain.ElectionRef) string {
	switch ref.Scope.Kind {
	case domain.ScopeBrazil:
		return "Brasil"
	case domain.ScopeState:
		if n, ok := stateNames[strings.ToLower(ref.Scope.UF)]; ok {
			return n + " (" + strings.ToUpper(ref.Scope.UF) + ")"
		}
		return strings.ToUpper(ref.Scope.UF)
	case domain.ScopeMunicipality:
		return "Município " + ref.Scope.Municipality + " (" + strings.ToUpper(ref.Scope.UF) + ")"
	default:
		return "Abrangência não informada"
	}
}

func sameCode(a, b string) bool {
	aa, errA := strconv.Atoi(strings.TrimLeft(a, "0"))
	bb, errB := strconv.Atoi(strings.TrimLeft(b, "0"))
	if errA == nil && errB == nil {
		return aa == bb
	}
	return strings.EqualFold(a, b)
}

func levelName(tp string) string {
	switch strings.TrimSpace(tp) {
	case "8":
		return "federal"
	case "1":
		return "estadual"
	case "3":
		return "municipal"
	case "4":
		return "suplementar"
	case "6", "7":
		return "consulta popular"
	default:
		return "não classificado"
	}
}

// resolveElection validates the requested ref against the official config and
// returns the metadata plus the result and municipality file URLs.
func (a *Adapter) resolveElection(cfg *wireConfig, ref domain.ElectionRef) (domain.ElectionInfo, string, error) {
	if cfg == nil {
		return domain.ElectionInfo{}, "", fmt.Errorf("configuração oficial não carregada")
	}
	uf := strings.ToLower(strings.TrimSpace(ref.Scope.UF))
	if uf == "" {
		uf = "br"
	}
	if ref.Scope.Kind == domain.ScopeBrazil {
		uf = "br"
	}

	for _, pleito := range cfg.PL {
		for _, ele := range pleito.E {
			if !sameCode(ele.CD, ref.ElectionCode) {
				continue
			}
			round, _ := strconv.Atoi(strings.TrimSpace(ele.T))
			if ref.Round != 0 && round != 0 && ref.Round != round {
				return domain.ElectionInfo{}, "", fmt.Errorf("eleição %s é do %s turno, não do %s turno", ele.CD, ele.T, strconv.Itoa(ref.Round))
			}
			if strings.TrimSpace(pleito.C) == "" {
				return domain.ElectionInfo{}, "", fmt.Errorf("ciclo ausente no config oficial para a eleição %s", ele.CD)
			}

			if ref.Scope.Kind == domain.ScopeBrazil && !strings.EqualFold(ele.TP, "8") {
				return domain.ElectionInfo{}, "", fmt.Errorf("a eleição %s não publica resultado nacional unificado; informe uma UF", ele.CD)
			}
			if !contestAllowed(ele, ref.ContestCode, uf, ref.Scope.Kind) {
				return domain.ElectionInfo{}, "", fmt.Errorf("cargo %s não existe na abrangência %s da eleição %s", ref.ContestCode, ref.Scope.Key(), ele.CD)
			}

			contestName := contestDescription(ele, ref.ContestCode)
			scopeKind := domain.ScopeBrazil
			if ref.Scope.Kind == domain.ScopeState {
				scopeKind = domain.ScopeState
			}
			if ref.Scope.Kind == domain.ScopeMunicipality {
				scopeKind = domain.ScopeMunicipality
			}
			normalized := domain.ElectionRef{
				Cycle:        pleito.C,
				ElectionCode: ele.CD,
				Round:        round,
				ContestCode:  ref.ContestCode,
				ContestName:  format.CleanText(contestName),
				ElectionName: format.CleanText(ele.NM),
				Level:        levelName(ele.TP),
				Scope: domain.Scope{
					Kind: scopeKind,
					UF:   uf,
				},
			}
			if scopeKind == domain.ScopeMunicipality {
				normalized.Scope.Municipality = padLeft(ref.Scope.Municipality, 5)
			}
			info := domain.ElectionInfo{
				Cycle:        normalized.Cycle,
				ElectionCode: normalized.ElectionCode,
				ElectionName: normalized.ElectionName,
				Round:        normalized.Round,
				ContestCode:  normalized.ContestCode,
				ContestName:  normalized.ContestName,
				Level:        normalized.Level,
				Scope:        normalized.Scope,
				ScopeName:    scopeName(normalized),
				SourcePage:   a.sourcePage(normalized),
			}
			resultURL := a.resultURL(normalized)
			return info, resultURL, nil
		}
	}
	return domain.ElectionInfo{}, "", fmt.Errorf("eleição %s não encontrada no config oficial", ref.ElectionCode)
}

func contestAllowed(ele wireElection, contest, uf string, kind domain.ScopeKind) bool {
	if kind == domain.ScopeBrazil {
		return contestInScope(ele, contest, "br")
	}
	uf = strings.ToLower(strings.TrimSpace(uf))
	if uf == "" || uf == "br" {
		return false
	}
	// The TSE publishes unified per-UF files for federal and state elections;
	// the official config may list only a national (br) scope. Municipality
	// results also use the UF directory. Therefore a contest is valid when it
	// appears in the exact UF scope or in the national scope.
	return contestInScope(ele, contest, uf) || contestInScope(ele, contest, "br")
}

func contestInScope(ele wireElection, contest, scope string) bool {
	for _, abr := range ele.ABR {
		if !strings.EqualFold(strings.TrimSpace(abr.CD), scope) {
			continue
		}
		for _, cp := range abr.CP {
			if sameCode(cp.CD, contest) {
				return true
			}
		}
	}
	return false
}

func contestDescription(ele wireElection, contest string) string {
	seen := map[string]bool{}
	for _, abr := range ele.ABR {
		for _, cp := range abr.CP {
			if sameCode(cp.CD, contest) && !seen[cp.DS] {
				seen[cp.DS] = true
				return cp.DS
			}
		}
	}
	return "cargo " + format.CleanText(contest)
}

func (a *Adapter) sourcePage(ref domain.ElectionRef) string {
	base := strings.TrimRight(a.BaseURL, "/") + "/" + a.Environment + "/app/index.html"
	if ref.Scope.Kind == domain.ScopeMunicipality {
		return base + "#/eleicao/" + ref.ElectionCode
	}
	uf := ref.Scope.UF
	if ref.Scope.Kind == domain.ScopeBrazil {
		uf = "br"
	}
	return base + "#/eleicao/" + ref.ElectionCode + "/uf/" + strings.ToLower(uf) + "/cargo/" + ref.ContestCode + "/vis/nominal/resultados"
}

func (a *Adapter) resultURL(ref domain.ElectionRef) string {
	uf := strings.ToLower(ref.Scope.UF)
	if ref.Scope.Kind == domain.ScopeBrazil || uf == "" {
		uf = "br"
	}
	prefix := uf
	if ref.Scope.Kind == domain.ScopeMunicipality && ref.Scope.Municipality != "" {
		prefix = uf + padLeft(ref.Scope.Municipality, 5)
	}
	file := prefix + "-c" + padLeft(ref.ContestCode, 4) + "-e" + padLeft(ref.ElectionCode, 6) + "-u.json"
	return strings.TrimRight(a.BaseURL, "/") + "/" + a.Environment + "/" + ref.Cycle + "/" + ref.ElectionCode + "/dados/" + uf + "/" + file
}

func padLeft(s string, n int) string {
	s = strings.TrimSpace(s)
	for len(s) < n {
		s = "0" + s
	}
	return s
}

func sortedElectionCodes(cfg *wireConfig) []string {
	var out []string
	for _, p := range cfg.PL {
		for _, e := range p.E {
			out = append(out, e.CD)
		}
	}
	sort.Strings(out)
	return out
}
