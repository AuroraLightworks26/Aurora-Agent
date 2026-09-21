package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sysagent/pkg/pipeline"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

type Store struct {
	conn *sql.DB
}

// PastMemory captures historical telemetry contextualized with a calculated relevance score.
type PastMemory struct {
	ID           int64
	Command      string
	Success      bool
	ErrorMessage string
	Similarity   float32
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed opening system db: %w", err)
	}

	s := &Store{conn: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.conn.Close()
}

func (s *Store) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS telemetry_logs (
		ID INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT,
		command TEXT,
		success INTEGER,
		error_message TEXT,
		embedding TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	_, err := s.conn.Exec(query)
	if err != nil {
		return fmt.Errorf("failed executing migration schema: %w", err)
	}
	return nil
}

func (s *Store) LogTaskResult(res pipeline.TaskResult, embedding []float32) error {
	query := `INSERT INTO telemetry_logs (task_id, command, success, error_message, embedding) VALUES (?, ?, ?, ?, ?);`

	successInt := 0
	if res.Success {
		successInt = 1
	}

	embeddingJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("failed to marshal vector embedding array: %w", err)
	}

	_, err = s.conn.Exec(query, res.ID, res.Command, successInt, res.ErrorMsg, string(embeddingJSON))
	if err != nil {
		return fmt.Errorf("failed inserting log metrics telemetry: %w", err)
	}
	return nil
}

// CalculateCosineSimilarity measures the angle between two float vectors.
func CalculateCosineSimilarity(vecA, vecB []float32) float32 {
	if len(vecA) == 0 || len(vecB) == 0 || len(vecA) != len(vecB) {
		return 0.0
	}
	var dotProduct, normA, normB float64
	for i := 0; i < len(vecA); i++ {
		dotProduct += float64(vecA[i] * vecB[i])
		normA += float64(vecA[i] * vecA[i])
		normB += float64(vecB[i] * vecB[i])
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// SearchRelevantMemories scans your historical database records to pull matching experiences.
func (s *Store) SearchRelevantMemories(queryVector []float32, similarityThreshold float32) ([]PastMemory, error) {
	// 1. ADD 'id' TO THE SQL SELECT STATEMENT (Now selecting 5 columns)
	rows, err := s.conn.Query("SELECT id, command, success, error_message, embedding FROM telemetry_logs")
	if err != nil {
		return nil, fmt.Errorf("failed scanning log rows: %w", err)
	}
	defer rows.Close()

	var memories []PastMemory

	for rows.Next() {
		// 2. ADD 'rowID' AS A DESTINATION VARIABLE
		var rowID int64
		var cmd, errMsg, embedStr string
		var successInt int

		// 3. PASS '&rowID' AS THE FIRST ARGUMENT TO MATCH THE SELECT ORDER (Scanning 5 variables)
		if err := rows.Scan(&rowID, &cmd, &successInt, &errMsg, &embedStr); err != nil {
			continue
		}

		if embedStr == "" || embedStr == "[]" {
			continue
		}

		var rowVector []float32
		if err := json.Unmarshal([]byte(embedStr), &rowVector); err != nil {
			continue
		}

		similarity := CalculateCosineSimilarity(queryVector, rowVector)
		if similarity >= similarityThreshold {
			// 4. ASSIGN THE EXTRACTED ID TO THE STRUCT LITERAL
			memories = append(memories, PastMemory{
				ID:           rowID,
				Command:      cmd,
				Success:      successInt == 1,
				ErrorMessage: errMsg,
				Similarity:   similarity,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error encountered during rows iteration: %w", err)
	}

	return memories, nil
}

// DeleteMemory removes a specific history entry from the telemetry_logs ledger by its primary key ID.
func (s *Store) DeleteMemory(id int64) error {
	_, err := s.conn.Exec("DELETE FROM telemetry_logs WHERE id = ?", id)
	return err
}
