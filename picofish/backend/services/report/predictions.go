// Package report — predictions.go implements confidence-scored prediction tracking.
// Predictions are extracted from the ReACT report via LLM, stored with
// confidence scores, and calibrated as outcomes are marked true/false.
package report

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"picofish/services/llm"
	"picofish/storage"

	"github.com/google/uuid"
)

// ── Types ──────────────────────────────────────────────────────────────────

// PredictionClaim is a single falsifiable prediction extracted from a report.
type PredictionClaim struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"project_id"`
	ReportID   string  `json:"report_id"`
	Claim      string  `json:"claim"`       // the falsifiable statement
	Confidence float64 `json:"confidence"`  // 0.0–1.0
	Timeframe  string  `json:"timeframe"`   // "short"|"medium"|"long"
	Category   string  `json:"category"`    // "social"|"political"|"economic"|"other"
	Outcome    *bool   `json:"outcome"`     // nil = not yet resolved, true = correct, false = incorrect
	OutcomeAt  string  `json:"outcome_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// CalibrationScore measures prediction accuracy across multiple resolved claims.
type CalibrationScore struct {
	ProjectID        string             `json:"project_id"`
	TotalPredictions int                `json:"total_predictions"`
	Resolved         int                `json:"resolved"`
	Correct          int                `json:"correct"`
	Accuracy         float64            `json:"accuracy"`         // correct / resolved
	AvgConfidence    float64            `json:"avg_confidence"`   // average stated confidence
	BrierScore       float64            `json:"brier_score"`      // lower = better calibrated
	ByConfidenceBand map[string]float64 `json:"by_confidence_band"` // "high|medium|low" → accuracy
}

// ── Extraction ─────────────────────────────────────────────────────────────

// ExtractPredictions uses LLM to parse falsifiable claims from a report text.
// Returns stored PredictionClaims with confidence scores.
func ExtractPredictions(ctx context.Context, projectID, reportID, reportText string) ([]*PredictionClaim, error) {
	prompt := fmt.Sprintf(`You are a prediction analyst. Read this simulation report and extract ONLY specific, falsifiable predictions — statements that could be verified as true or false in the future.

Report:
%s

Extract up to 10 predictions. For each, assign:
- confidence: 0.0–1.0 (your confidence this will happen)
- timeframe: "short" (days–weeks), "medium" (1–3 months), or "long" (3+ months)
- category: "social", "political", "economic", or "other"

Respond with a JSON array:
[
  {
    "claim": "specific falsifiable statement",
    "confidence": 0.75,
    "timeframe": "medium",
    "category": "social"
  }
]

Only include predictions explicitly supported by the report. Do NOT add predictions not in the text.`, truncateText(reportText, 3000))

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)},
		llm.WithTemperature(0.2), llm.WithMaxTokens(800))
	if err != nil {
		return nil, fmt.Errorf("llm extract: %w", err)
	}

	var raw []struct {
		Claim      string  `json:"claim"`
		Confidence float64 `json:"confidence"`
		Timeframe  string  `json:"timeframe"`
		Category   string  `json:"category"`
	}
	if err := llm.ParseJSON(resp, &raw); err != nil {
		return nil, fmt.Errorf("parse predictions: %w", err)
	}

	claims := make([]*PredictionClaim, 0, len(raw))
	for _, r := range raw {
		if r.Claim == "" {
			continue
		}
		c := &PredictionClaim{
			ID:         uuid.NewString(),
			ProjectID:  projectID,
			ReportID:   reportID,
			Claim:      r.Claim,
			Confidence: clampF(r.Confidence, 0.0, 1.0),
			Timeframe:  r.Timeframe,
			Category:   r.Category,
			CreatedAt:  time.Now().Format(time.RFC3339),
		}
		claims = append(claims, c)
	}

	if err := SavePredictions(claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// ── Storage ────────────────────────────────────────────────────────────────

// SavePredictions persists a slice of prediction claims.
func SavePredictions(claims []*PredictionClaim) error {
	for _, c := range claims {
		b, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if err := storage.DB.Insert("predictions", c.ID, storage.Record{
			"id":         c.ID,
			"project_id": c.ProjectID,
			"report_id":  c.ReportID,
			"type":       "prediction",
			"data":       string(b),
		}); err != nil {
			return err
		}
	}
	return nil
}

// GetPredictions returns all predictions for a project, newest first.
func GetPredictions(projectID string) ([]*PredictionClaim, error) {
	records := storage.DB.QueryFunc("predictions", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID &&
			storage.GetStr(r, "type") == "prediction"
	})
	claims := make([]*PredictionClaim, 0, len(records))
	for _, r := range records {
		var c PredictionClaim
		if err := json.Unmarshal([]byte(storage.GetStr(r, "data")), &c); err == nil {
			claims = append(claims, &c)
		}
	}
	return claims, nil
}

// MarkOutcome records whether a prediction was correct or not.
func MarkOutcome(predictionID string, correct bool) error {
	rec, ok := storage.DB.Get("predictions", predictionID)
	if !ok {
		return fmt.Errorf("prediction %s not found", predictionID)
	}

	var c PredictionClaim
	if err := json.Unmarshal([]byte(storage.GetStr(rec, "data")), &c); err != nil {
		return err
	}

	c.Outcome = &correct
	c.OutcomeAt = time.Now().Format(time.RFC3339)

	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return storage.DB.Update("predictions", predictionID, storage.Record{
		"data": string(b),
	})
}

// ── Calibration ────────────────────────────────────────────────────────────

// CalibrationScore computes prediction accuracy and Brier score for a project.
func GetCalibrationScore(projectID string) (*CalibrationScore, error) {
	claims, err := GetPredictions(projectID)
	if err != nil {
		return nil, err
	}

	score := &CalibrationScore{
		ProjectID:        projectID,
		TotalPredictions: len(claims),
		ByConfidenceBand: make(map[string]float64),
	}
	if len(claims) == 0 {
		return score, nil
	}

	// Calibration bands: high ≥ 0.7, medium 0.4–0.7, low < 0.4
	bandCorrect := map[string]int{"high": 0, "medium": 0, "low": 0}
	bandTotal := map[string]int{"high": 0, "medium": 0, "low": 0}

	totalConf := 0.0
	brierSum := 0.0
	resolved := 0
	correct := 0

	for _, c := range claims {
		totalConf += c.Confidence
		if c.Outcome == nil {
			continue
		}
		resolved++
		outcome := 0.0
		if *c.Outcome {
			outcome = 1.0
			correct++
		}
		diff := c.Confidence - outcome
		brierSum += diff * diff

		band := confidenceBand(c.Confidence)
		bandTotal[band]++
		if *c.Outcome {
			bandCorrect[band]++
		}
	}

	score.Resolved = resolved
	score.Correct = correct
	score.AvgConfidence = roundF(totalConf/float64(len(claims)), 3)

	if resolved > 0 {
		score.Accuracy = roundF(float64(correct)/float64(resolved), 3)
		score.BrierScore = roundF(brierSum/float64(resolved), 4)
	}

	for band, total := range bandTotal {
		if total > 0 {
			score.ByConfidenceBand[band] = roundF(float64(bandCorrect[band])/float64(total), 3)
		}
	}
	return score, nil
}

// ── Helpers ────────────────────────────────────────────────────────────────

func confidenceBand(c float64) string {
	if c >= 0.7 {
		return "high"
	} else if c >= 0.4 {
		return "medium"
	}
	return "low"
}

func clampF(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func roundF(f float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(f*pow) / pow
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
