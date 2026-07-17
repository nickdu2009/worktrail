package eval

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
)

const (
	CorpusSchema  = "worktrail.semantic.eval.corpus.v1"
	CaptureSchema = "worktrail.semantic.eval.capture.v1"
	ReportSchema  = "worktrail.semantic.eval.report.v1"
)

type Corpus struct {
	Schema    string     `json:"schema"`
	Queries   []Query    `json:"queries"`
	Documents []Document `json:"documents"`
}

type Query struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	RelevantIDs []string `json:"relevant_ids"`
}

type Document struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type EmbeddingConfig struct {
	Dimension        int    `json:"dimension"`
	Tokenizer        string `json:"tokenizer"`
	Pooling          string `json:"pooling"`
	Normalization    string `json:"normalization"`
	QueryTemplate    string `json:"query_template"`
	DocumentTemplate string `json:"document_template"`
}

type Capture struct {
	Schema         string          `json:"schema"`
	Provider       string          `json:"provider"`
	Model          string          `json:"model"`
	Revision       string          `json:"revision"`
	ArtifactSHA256 string          `json:"artifact_sha256"`
	Config         EmbeddingConfig `json:"config"`
	Embeddings     []Embedding     `json:"embeddings"`
}

type Embedding struct {
	ID     string    `json:"id"`
	Single []float64 `json:"single"`
	Batch  []float64 `json:"batch"`
}

type Thresholds struct {
	K              int     `json:"k"`
	NormTolerance  float64 `json:"norm_tolerance"`
	MinCosine      float64 `json:"min_cosine"`
	MaxBatchDelta  float64 `json:"max_batch_delta"`
	MinTopKOverlap float64 `json:"min_top_k_overlap"`
	MinRecallAtK   float64 `json:"min_recall_at_k"`
	MinMRR         float64 `json:"min_mrr"`
	MinNDCGAtK     float64 `json:"min_ndcg_at_k"`
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		K:              10,
		NormTolerance:  1e-5,
		MinCosine:      0.999,
		MaxBatchDelta:  2e-4,
		MinTopKOverlap: 0.9,
		MinRecallAtK:   0.9,
		MinMRR:         0.9,
		MinNDCGAtK:     0.9,
	}
}

type Report struct {
	Schema         string          `json:"schema"`
	Reference      CaptureIdentity `json:"reference"`
	Candidate      CaptureIdentity `json:"candidate"`
	Thresholds     Thresholds      `json:"thresholds"`
	Entries        int             `json:"entries"`
	Queries        int             `json:"queries"`
	MinCosine      float64         `json:"min_cosine"`
	MaxBatchDelta  float64         `json:"max_batch_delta"`
	TopKOverlap    float64         `json:"top_k_overlap"`
	RecallAtK      float64         `json:"recall_at_k"`
	MRR            float64         `json:"mrr"`
	NDCGAtK        float64         `json:"ndcg_at_k"`
	Passed         bool            `json:"passed"`
	FailureReasons []string        `json:"failure_reasons,omitempty"`
}

type CaptureIdentity struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Revision       string `json:"revision"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}

func DecodeCorpus(r io.Reader) (Corpus, error) {
	var corpus Corpus
	if err := decodeOne(r, &corpus); err != nil {
		return Corpus{}, err
	}
	if err := validateCorpus(corpus); err != nil {
		return Corpus{}, err
	}
	return corpus, nil
}

func DecodeCapture(r io.Reader) (Capture, error) {
	var capture Capture
	if err := decodeOne(r, &capture); err != nil {
		return Capture{}, err
	}
	if err := validateCapture(capture); err != nil {
		return Capture{}, err
	}
	return capture, nil
}

func Compare(corpus Corpus, reference, candidate Capture, thresholds Thresholds) (Report, error) {
	if err := validateCorpus(corpus); err != nil {
		return Report{}, err
	}
	if err := validateCapture(reference); err != nil {
		return Report{}, fmt.Errorf("reference: %w", err)
	}
	if err := validateCapture(candidate); err != nil {
		return Report{}, fmt.Errorf("candidate: %w", err)
	}
	if reference.Config != candidate.Config {
		return Report{}, errors.New("embedding config mismatch")
	}
	if err := validateThresholds(thresholds); err != nil {
		return Report{}, err
	}

	allIDs := corpusIDs(corpus)
	ref, err := captureMap(reference, allIDs)
	if err != nil {
		return Report{}, fmt.Errorf("reference: %w", err)
	}
	cand, err := captureMap(candidate, allIDs)
	if err != nil {
		return Report{}, fmt.Errorf("candidate: %w", err)
	}

	report := Report{
		Schema:     ReportSchema,
		Reference:  identity(reference),
		Candidate:  identity(candidate),
		Thresholds: thresholds,
		Entries:    len(allIDs),
		Queries:    len(corpus.Queries),
		MinCosine:  1,
	}
	for _, id := range allIDs {
		if err := validateVector(ref[id].Single, reference.Config.Dimension, thresholds.NormTolerance); err != nil {
			return Report{}, fmt.Errorf("reference embedding %q: %w", id, err)
		}
		if err := validateVector(ref[id].Batch, reference.Config.Dimension, thresholds.NormTolerance); err != nil {
			return Report{}, fmt.Errorf("reference batch embedding %q: %w", id, err)
		}
		if err := validateVector(cand[id].Single, candidate.Config.Dimension, thresholds.NormTolerance); err != nil {
			return Report{}, fmt.Errorf("candidate embedding %q: %w", id, err)
		}
		if err := validateVector(cand[id].Batch, candidate.Config.Dimension, thresholds.NormTolerance); err != nil {
			return Report{}, fmt.Errorf("candidate batch embedding %q: %w", id, err)
		}
		report.MinCosine = min(report.MinCosine, cosine(ref[id].Single, cand[id].Single))
		report.MaxBatchDelta = max(report.MaxBatchDelta, maxAbsDelta(ref[id].Single, ref[id].Batch))
		report.MaxBatchDelta = max(report.MaxBatchDelta, maxAbsDelta(cand[id].Single, cand[id].Batch))
	}

	report.TopKOverlap, report.RecallAtK, report.MRR, report.NDCGAtK = retrievalMetrics(corpus, ref, cand, thresholds.K)
	if report.MinCosine < thresholds.MinCosine {
		report.FailureReasons = append(report.FailureReasons, "min_cosine")
	}
	if report.MaxBatchDelta > thresholds.MaxBatchDelta {
		report.FailureReasons = append(report.FailureReasons, "max_batch_delta")
	}
	if report.TopKOverlap < thresholds.MinTopKOverlap {
		report.FailureReasons = append(report.FailureReasons, "top_k_overlap")
	}
	if report.RecallAtK < thresholds.MinRecallAtK {
		report.FailureReasons = append(report.FailureReasons, "recall_at_k")
	}
	if report.MRR < thresholds.MinMRR {
		report.FailureReasons = append(report.FailureReasons, "mrr")
	}
	if report.NDCGAtK < thresholds.MinNDCGAtK {
		report.FailureReasons = append(report.FailureReasons, "ndcg_at_k")
	}
	report.Passed = len(report.FailureReasons) == 0
	return report, nil
}

func decodeOne(r io.Reader, out any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("expected one JSON document")
	}
	return nil
}

func validateCorpus(corpus Corpus) error {
	if corpus.Schema != CorpusSchema {
		return fmt.Errorf("unexpected corpus schema %q", corpus.Schema)
	}
	if len(corpus.Queries) == 0 || len(corpus.Documents) == 0 {
		return errors.New("corpus requires queries and documents")
	}
	seen := map[string]bool{}
	docIDs := map[string]bool{}
	for _, doc := range corpus.Documents {
		if doc.ID == "" || doc.Text == "" || seen[doc.ID] {
			return fmt.Errorf("invalid or duplicate document id %q", doc.ID)
		}
		seen[doc.ID], docIDs[doc.ID] = true, true
	}
	for _, query := range corpus.Queries {
		if query.ID == "" || query.Text == "" || seen[query.ID] || len(query.RelevantIDs) == 0 {
			return fmt.Errorf("invalid or duplicate query id %q", query.ID)
		}
		seen[query.ID] = true
		for _, id := range query.RelevantIDs {
			if !docIDs[id] {
				return fmt.Errorf("query %q references unknown document %q", query.ID, id)
			}
		}
	}
	return nil
}

func validateCapture(capture Capture) error {
	if capture.Schema != CaptureSchema {
		return fmt.Errorf("unexpected capture schema %q", capture.Schema)
	}
	if capture.Provider == "" || capture.Model == "" || capture.Revision == "" || capture.ArtifactSHA256 == "" {
		return errors.New("capture identity is incomplete")
	}
	if !isSHA256(capture.ArtifactSHA256) {
		return errors.New("artifact_sha256 must be a lowercase 64-character SHA-256")
	}
	if capture.Config.Dimension <= 0 || capture.Config.Tokenizer == "" || capture.Config.Pooling == "" ||
		capture.Config.Normalization == "" || capture.Config.QueryTemplate == "" || capture.Config.DocumentTemplate == "" {
		return errors.New("embedding config is incomplete")
	}
	if len(capture.Embeddings) == 0 {
		return errors.New("capture has no embeddings")
	}
	return nil
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func validateThresholds(t Thresholds) error {
	if t.K <= 0 || t.NormTolerance < 0 || t.MaxBatchDelta < 0 {
		return errors.New("invalid non-positive threshold")
	}
	for _, value := range []float64{t.MinCosine, t.MinTopKOverlap, t.MinRecallAtK, t.MinMRR, t.MinNDCGAtK} {
		if value < 0 || value > 1 {
			return errors.New("quality thresholds must be between zero and one")
		}
	}
	return nil
}

func corpusIDs(corpus Corpus) []string {
	ids := make([]string, 0, len(corpus.Queries)+len(corpus.Documents))
	for _, query := range corpus.Queries {
		ids = append(ids, query.ID)
	}
	for _, doc := range corpus.Documents {
		ids = append(ids, doc.ID)
	}
	sort.Strings(ids)
	return ids
}

func captureMap(capture Capture, expected []string) (map[string]Embedding, error) {
	embeddings := make(map[string]Embedding, len(capture.Embeddings))
	for _, embedding := range capture.Embeddings {
		if embedding.ID == "" || embeddings[embedding.ID].ID != "" {
			return nil, fmt.Errorf("invalid or duplicate embedding id %q", embedding.ID)
		}
		embeddings[embedding.ID] = embedding
	}
	if len(embeddings) != len(expected) {
		return nil, fmt.Errorf("embedding count %d does not match corpus count %d", len(embeddings), len(expected))
	}
	for _, id := range expected {
		if embeddings[id].ID == "" {
			return nil, fmt.Errorf("missing embedding %q", id)
		}
	}
	return embeddings, nil
}

func validateVector(vector []float64, dimension int, tolerance float64) error {
	if len(vector) != dimension {
		return fmt.Errorf("dimension %d, want %d", len(vector), dimension)
	}
	var squared float64
	for _, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("vector contains non-finite value")
		}
		squared += value * value
	}
	if math.Abs(math.Sqrt(squared)-1) > tolerance {
		return fmt.Errorf("vector norm outside tolerance: %.12f", math.Sqrt(squared))
	}
	return nil
}

func retrievalMetrics(corpus Corpus, reference, candidate map[string]Embedding, k int) (float64, float64, float64, float64) {
	var overlap, recall, mrr, ndcg float64
	for _, query := range corpus.Queries {
		refRank := rankDocuments(query.ID, corpus.Documents, reference)
		candRank := rankDocuments(query.ID, corpus.Documents, candidate)
		limit := min(k, len(corpus.Documents))
		overlap += topKOverlap(refRank[:limit], candRank[:limit])
		relevant := make(map[string]bool, len(query.RelevantIDs))
		for _, id := range query.RelevantIDs {
			relevant[id] = true
		}
		recall += recallAtK(candRank[:limit], relevant)
		mrr += reciprocalRank(candRank, relevant)
		ndcg += ndcgAtK(candRank[:limit], relevant)
	}
	count := float64(len(corpus.Queries))
	return overlap / count, recall / count, mrr / count, ndcg / count
}

func rankDocuments(queryID string, documents []Document, embeddings map[string]Embedding) []string {
	type scored struct {
		id    string
		score float64
	}
	items := make([]scored, 0, len(documents))
	for _, doc := range documents {
		items = append(items, scored{id: doc.ID, score: cosine(embeddings[queryID].Single, embeddings[doc.ID].Single)})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].id < items[j].id
		}
		return items[i].score > items[j].score
	})
	ranked := make([]string, len(items))
	for i, item := range items {
		ranked[i] = item.id
	}
	return ranked
}

func cosine(a, b []float64) float64 {
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

func maxAbsDelta(a, b []float64) float64 {
	var delta float64
	for i := range a {
		delta = max(delta, math.Abs(a[i]-b[i]))
	}
	return delta
}

func topKOverlap(a, b []string) float64 {
	set := make(map[string]bool, len(a))
	for _, id := range a {
		set[id] = true
	}
	var matches int
	for _, id := range b {
		if set[id] {
			matches++
		}
	}
	return float64(matches) / float64(len(a))
}

func recallAtK(ranked []string, relevant map[string]bool) float64 {
	var found int
	for _, id := range ranked {
		if relevant[id] {
			found++
		}
	}
	return float64(found) / float64(len(relevant))
}

func reciprocalRank(ranked []string, relevant map[string]bool) float64 {
	for i, id := range ranked {
		if relevant[id] {
			return 1 / float64(i+1)
		}
	}
	return 0
}

func ndcgAtK(ranked []string, relevant map[string]bool) float64 {
	var dcg float64
	for i, id := range ranked {
		if relevant[id] {
			dcg += 1 / math.Log2(float64(i)+2)
		}
	}
	idealCount := min(len(relevant), len(ranked))
	var ideal float64
	for i := 0; i < idealCount; i++ {
		ideal += 1 / math.Log2(float64(i)+2)
	}
	if ideal == 0 {
		return 0
	}
	return dcg / ideal
}

func identity(capture Capture) CaptureIdentity {
	return CaptureIdentity{
		Provider:       capture.Provider,
		Model:          capture.Model,
		Revision:       capture.Revision,
		ArtifactSHA256: capture.ArtifactSHA256,
	}
}
