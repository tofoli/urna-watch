package tse

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/format"
)

type wireResult struct {
	Ele   string `json:"ele"`
	T     string `json:"t"`
	F     string `json:"f"`
	Sup   string `json:"sup"`
	TPAbr string `json:"tpabr"`
	CDAbr string `json:"cdabr"`
	DG    string `json:"dg"`
	HG    string `json:"hg"`
	IDG   string `json:"idg"`
	DT    string `json:"dt"`
	HT    string `json:"ht"`
	DV    string `json:"dv"`
	TF    string `json:"tf"`
	And   string `json:"and"`
	MD    string `json:"md"`
	ESAE  string `json:"esae"`

	S    *wireSections   `json:"s"`
	V    *wireVotes      `json:"v"`
	E    *wireElectorate `json:"e"`
	Carg []wireCargo     `json:"carg"`
}

type wireSections struct {
	TS   string `json:"ts"`
	ST   string `json:"st"`
	PST  string `json:"pst"`
	PSTN string `json:"pstn"`
	SNT  string `json:"snt"`
	PSNT string `json:"psnt"`
	SI   string `json:"si"`
	PSI  string `json:"psi"`
	SA   string `json:"sa"`
	PSA  string `json:"psa"`
}

type wireVotes struct {
	TV    string `json:"tv"`
	VV    string `json:"vv"`
	PVV   string `json:"pvv"`
	VNOM  string `json:"vnom"`
	PVNOM string `json:"pvnom"`
	VB    string `json:"vb"`
	PVB   string `json:"pvb"`
	VN    string `json:"vn"`
	PVN   string `json:"pvn"`
	VNT   string `json:"vnt"`
	PVNT  string `json:"pvnt"`
}

type wireElectorate struct {
	TE  string `json:"te"`
	EST string `json:"est"`
	ESI string `json:"esi"`
	ESA string `json:"esa"`
	C   string `json:"c"`
	PC  string `json:"pc"`
	A   string `json:"a"`
	PA  string `json:"pa"`
}

type wireCargo struct {
	CD  string    `json:"cd"`
	NMN string    `json:"nmn"`
	NMM string    `json:"nmm"`
	NMF string    `json:"nmf"`
	NV  string    `json:"nv"`
	Agr []wireAgr `json:"agr"`
}

type wireAgr struct {
	N   string      `json:"n"`
	NM  string      `json:"nm"`
	TP  string      `json:"tp"`
	COM string      `json:"com"`
	Par []wireParty `json:"par"`
}

type wireParty struct {
	N    string          `json:"n"`
	SG   string          `json:"sg"`
	NM   string          `json:"nm"`
	NFED string          `json:"nfed"`
	TVTN string          `json:"tvtn"`
	TVAN string          `json:"tvan"`
	Cand []wireCandidate `json:"cand"`
}

type wireCandidate struct {
	N      string `json:"n"`
	SQCand string `json:"sqcand"`
	NM     string `json:"nm"`
	NMU    string `json:"nmu"`
	DT     string `json:"dt"`
	DVT    string `json:"dvt"`
	SEQ    string `json:"seq"`
	E      string `json:"e"`
	ST     string `json:"st"`
	VAP    string `json:"vap"`
	PVAP   string `json:"pvap"`
	PVAPN  string `json:"pvapn"`
	SGP    string `json:"sgp"`
	COM    string `json:"com"`
}

// ParseResult converts an official EA20 result file into the internal model.
// It is pure and does not perform network access.
func ParseResult(body []byte, info domain.ElectionInfo, sourceURL string, now time.Time) (*domain.Snapshot, error) {
	var root wireResult
	dec := json.NewDecoder(strings.NewReader(string(body)))
	if err := dec.Decode(&root); err != nil {
		return nil, &ParseError{Reason: "JSON inválido", Err: err}
	}
	if strings.TrimSpace(root.IDG) == "" {
		return nil, &ParseError{Reason: "campo idg ausente"}
	}
	if len(root.Carg) == 0 {
		return nil, &ParseError{Reason: "campo carg ausente ou vazio"}
	}

	loc := sourceLocation()
	generated, err := parseSourceTime(root.DG, root.HG, loc)
	if err != nil {
		return nil, &ParseError{Reason: "data de geração inválida", Err: err}
	}
	totalized, err := parseSourceTime(firstNonEmpty(root.DT, root.DG), firstNonEmpty(root.HT, root.HG), loc)
	if err != nil {
		return nil, &ParseError{Reason: "data de totalização inválida", Err: err}
	}

	snap := &domain.Snapshot{
		Election:       info,
		CapturedAt:     now.In(loc),
		SourceVersion:  strings.TrimSpace(root.IDG),
		GeneratedAt:    generated,
		TotalizedAt:    totalized,
		IsFinal:        equalS(root.TF),
		VotesPublished: equalS(root.DV),
		SourceURL:      sourceURL,
	}
	if snap.IsFinal {
		snap.StateText = "final"
	} else {
		snap.StateText = "parcial"
	}

	if root.S != nil {
		if v, ok, err := parseOptionalInt(root.S.TS); err != nil {
			return nil, &ParseError{Reason: "total de seções inválido", Err: err}
		} else if ok {
			snap.SectionsTotal = domain.Known(v)
		}
		if v, ok, err := parseOptionalInt(root.S.ST); err != nil {
			return nil, &ParseError{Reason: "seções totalizadas inválidas", Err: err}
		} else if ok {
			snap.SectionsCounted = domain.Known(v)
		}
		if v, ok, err := parseNanoPercent(firstNonEmpty(root.S.PSTN, root.S.PST)); err != nil {
			return nil, &ParseError{Reason: "percentual de seções inválido", Err: err}
		} else if ok {
			snap.SectionsPercentNano = v
			snap.SectionsPercentText = firstNonEmpty(strings.TrimSpace(root.S.PST), format.PercentNano(v))
		}
	}
	if root.V != nil {
		snap.Totals = domain.Totals{
			TotalVotes:   mustMeasure(root.V.TV),
			ValidVotes:   mustMeasure(root.V.VV),
			NominalVotes: mustMeasure(root.V.VNOM),
			BlankVotes:   mustMeasure(root.V.VB),
			NullVotes:    mustMeasure(root.V.VN),
		}
	}
	if root.E != nil {
		snap.Totals.Electorate = mustMeasure(root.E.TE)
		snap.Totals.Turnout = mustMeasure(root.E.C)
		snap.Totals.Abstention = mustMeasure(root.E.A)
	}

	cargo, err := selectCargo(root.Carg, info.ContestCode)
	if err != nil {
		return nil, &ParseError{Reason: err.Error()}
	}
	if strings.TrimSpace(info.ContestName) == "" || info.ContestName == "cargo "+info.ContestCode {
		if name := strings.TrimSpace(cargo.NMN); name != "" {
			snap.Election.ContestName = name
		}
	}

	seen := make(map[string]bool)
	for _, agr := range cargo.Agr {
		for _, party := range agr.Par {
			for _, c := range party.Cand {
				number := strings.TrimSpace(c.N)
				if number == "" {
					return nil, &ParseError{Reason: "candidato sem número eleitoral"}
				}
				if seen[number] {
					continue
				}
				name := format.CleanText(strings.TrimSpace(c.NM))
				if name == "" {
					name = format.CleanText(strings.TrimSpace(c.NMU))
				}
				if name == "" {
					return nil, &ParseError{Reason: "candidato " + number + " sem nome"}
				}
				votes, known, err := parseOptionalInt(c.VAP)
				if err != nil {
					return nil, &ParseError{Reason: "votos inválidos do candidato " + number, Err: err}
				}
				pctNano, pctKnown, err := parseNanoPercent(firstNonEmpty(c.PVAPN, c.PVAP))
				if err != nil {
					return nil, &ParseError{Reason: "percentual inválido do candidato " + number, Err: err}
				}
				if pctKnown && (pctNano < 0 || pctNano > 100_000_000_000) {
					return nil, &ParseError{Reason: "percentual fora de 0–100 do candidato " + number}
				}
				partyAcronym := format.CleanText(firstNonEmpty(strings.TrimSpace(c.SGP), strings.TrimSpace(party.SG)))
				partyName := format.CleanText(firstNonEmpty(strings.TrimSpace(party.NM), strings.TrimSpace(agr.NM)))
				status := format.CleanText(strings.TrimSpace(c.ST))
				candidate := domain.Candidate{
					Number:       number,
					Name:         name,
					BallotName:   format.CleanText(strings.TrimSpace(c.NMU)),
					Party:        partyName,
					PartyAcronym: partyAcronym,
					Votes:        domain.Measure{Value: votes, Known: known},
					PercentNano:  pctNano,
					PercentText:  format.CleanText(firstNonEmpty(strings.TrimSpace(c.PVAP), format.PercentNano(pctNano))),
					Status:       status,
					Elected:      electedFromSource(c.E, status),
				}
				snap.Candidates = append(snap.Candidates, candidate)
				seen[number] = true
			}
		}
	}
	if len(snap.Candidates) == 0 {
		return nil, &ParseError{Reason: "nenhum candidato encontrado para o cargo " + info.ContestCode}
	}
	snap.SortCandidates()
	if err := validateSnapshot(snap); err != nil {
		return nil, err
	}
	snap.ID = snapshotID(snap)
	return snap, nil
}

func selectCargo(cargos []wireCargo, contest string) (wireCargo, error) {
	for _, c := range cargos {
		if sameCode(c.CD, contest) {
			return c, nil
		}
	}
	if len(cargos) == 1 {
		return cargos[0], nil
	}
	return wireCargo{}, fmt.Errorf("cargo %s não encontrado no arquivo de resultados", contest)
}

func validateSnapshot(s *domain.Snapshot) error {
	if s.SourceVersion == "" {
		return &ParseError{Reason: "versão da fonte vazia"}
	}
	if s.SectionsTotal.Known && s.SectionsCounted.Known && s.SectionsCounted.Value > s.SectionsTotal.Value {
		return &ParseError{Reason: "seções totalizadas excedem o total de seções"}
	}
	if s.SectionsPercentNano < 0 || s.SectionsPercentNano > 100_000_000_000 {
		return &ParseError{Reason: "percentual de seções fora de 0–100"}
	}
	valid := s.Totals.ValidVotes
	var sum int64
	for _, c := range s.Candidates {
		if !c.Votes.Known || c.Votes.Value < 0 {
			return &ParseError{Reason: "votos ausentes ou negativos para o candidato " + c.Number}
		}
		sum += c.Votes.Value
		if valid.Known && valid.Value > 0 && c.Votes.Value > valid.Value+(valid.Value/50)+100 {
			return &ParseError{Reason: "votos do candidato " + c.Number + " excedem os votos válidos"}
		}
	}
	if valid.Known && valid.Value > 0 && sum > valid.Value+(valid.Value/50)+100 {
		return &ParseError{Reason: "soma dos votos dos candidatos excede os votos válidos"}
	}
	if s.Totals.TotalVotes.Known && s.Totals.TotalVotes.Value > 0 && s.SectionsCounted.OrZero() > 0 && sum == 0 {
		if s.VotesPublished {
			return &ParseError{Reason: "votação publicada sem votos de candidatos"}
		}
	}
	if s.Totals.TotalVotes.Known && s.Totals.ValidVotes.Known && s.Totals.BlankVotes.Known && s.Totals.NullVotes.Known {
		parts := s.Totals.ValidVotes.Value + s.Totals.BlankVotes.Value + s.Totals.NullVotes.Value
		total := s.Totals.TotalVotes.Value
		if total > 0 && parts > total+(total/100)+1000 {
			return &ParseError{Reason: "votos válidos + brancos + nulos excedem a votação total"}
		}
	}
	if s.IsFinal && s.SectionsTotal.Known && s.SectionsCounted.Known && s.SectionsCounted.Value < s.SectionsTotal.Value {
		// The source may publish a final flag before the local CDN file is
		// updated, but we refuse to present obviously inconsistent data.
		return &ParseError{Reason: "totalização final com seções pendentes"}
	}
	return nil
}

func snapshotID(s *domain.Snapshot) string {
	sum := sha256.Sum256([]byte(s.Election.Cycle + "\x00" + s.Election.ElectionCode + "\x00" + s.Election.ContestCode + "\x00" + s.Election.Scope.Key() + "\x00" + s.Fingerprint()))
	return hex.EncodeToString(sum[:16])
}

func electedFromSource(e, status string) *bool {
	e = strings.ToLower(strings.TrimSpace(e))
	status = strings.ToLower(strings.TrimSpace(status))
	if e == "" {
		return nil
	}
	if e != "s" {
		v := false
		return &v
	}
	if strings.Contains(status, "não eleito") || strings.Contains(status, "nao eleito") {
		v := false
		return &v
	}
	if strings.Contains(status, "turno") {
		// "2º turno" means qualified for the runoff, not elected.
		v := false
		return &v
	}
	v := true
	return &v
}

func parseSourceTime(date, clock string, loc *time.Location) (time.Time, error) {
	date = strings.TrimSpace(date)
	clock = strings.TrimSpace(clock)
	if date == "" && clock == "" {
		return time.Time{}, nil
	}
	if date == "" || clock == "" {
		return time.Time{}, fmt.Errorf("data/hora incompleta: %q %q", date, clock)
	}
	t, err := time.ParseInLocation("02/01/2006 15:04:05", date+" "+clock, loc)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func sourceLocation() *time.Location {
	if loc, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		return loc
	}
	return time.FixedZone("BRT", -3*60*60)
}

func equalS(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "s")
}

func mustMeasure(v string) domain.Measure {
	n, ok, err := parseOptionalInt(v)
	if err != nil || !ok {
		return domain.Measure{}
	}
	return domain.Known(n)
}

func parseOptionalInt(v string) (int64, bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false, nil
	}
	v = strings.ReplaceAll(v, ".", "")
	v = strings.ReplaceAll(v, " ", "")
	v = strings.ReplaceAll(v, "\u00a0", "")
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

// parseNanoPercent parses the exact percentage published by the TSE (for
// example "57,655409048" or "57.655409048") into nano-percent units. No
// floating point comparison is used anywhere in the pipeline.
func parseNanoPercent(v string) (int64, bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false, nil
	}
	v = strings.ReplaceAll(v, ",", ".")
	neg := strings.HasPrefix(v, "-")
	v = strings.TrimPrefix(v, "-")
	parts := strings.SplitN(v, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false, err
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) > 9 {
		frac = frac[:9]
	}
	for len(frac) < 9 {
		frac += "0"
	}
	var fracN int64
	if frac != "" {
		fracN, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, false, err
		}
	}
	n := whole*1_000_000_000 + fracN
	if n < 0 || n > math.MaxInt64 {
		return 0, false, fmt.Errorf("percentual fora do intervalo")
	}
	if neg {
		n = -n
	}
	return n, true, nil
}
