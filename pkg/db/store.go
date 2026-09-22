package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sysagent/pkg/pipeline"
	"time"

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

// SystemProfileEntry represents a cached fact about the host machine.
type SystemProfileEntry struct {
	Category  string    `json:"category"`
	Attribute string    `json:"attribute"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SystemQuirk represents a learned behavior, library trap, or quirk.
type SystemQuirk struct {
	ID          int64     `json:"id"`
	ContextKey  string    `json:"context_key"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
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

	queryProfile := `
	CREATE TABLE IF NOT EXISTS system_profile (
		category TEXT,
		attribute TEXT,
		value TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (category, attribute)
	);`
	if _, err := s.conn.Exec(queryProfile); err != nil {
		return fmt.Errorf("failed executing system_profile migration schema: %w", err)
	}

	queryQuirks := `
	CREATE TABLE IF NOT EXISTS system_quirks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		context_key TEXT,
		description TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := s.conn.Exec(queryQuirks); err != nil {
		return fmt.Errorf("failed executing system_quirks migration schema: %w", err)
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

// SetSystemProfile upserts a fact into the agent's mental model of the machine.
func (s *Store) SetSystemProfile(category, attribute, value string) error {
	query := `
	INSERT INTO system_profile (category, attribute, value, updated_at)
	VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(category, attribute) DO UPDATE SET 
		value = excluded.value,
		updated_at = CURRENT_TIMESTAMP;
	`
	_, err := s.conn.Exec(query, category, attribute, value)
	if err != nil {
		return fmt.Errorf("failed to set system profile: %w", err)
	}
	return nil
}

// GetSystemProfile retrieves all cached facts about the host machine.
func (s *Store) GetSystemProfile() ([]SystemProfileEntry, error) {
	rows, err := s.conn.Query("SELECT category, attribute, value, updated_at FROM system_profile ORDER BY category;")
	if err != nil {
		return nil, fmt.Errorf("failed to query system profile: %w", err)
	}
	defer rows.Close()

	var entries []SystemProfileEntry
	for rows.Next() {
		var e SystemProfileEntry
		if err := rows.Scan(&e.Category, &e.Attribute, &e.Value, &e.UpdatedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// LogSystemQuirk records an operational quirk or library trap.
func (s *Store) LogSystemQuirk(contextKey, description string) error {
	query := `INSERT INTO system_quirks (context_key, description, timestamp) VALUES (?, ?, CURRENT_TIMESTAMP);`
	_, err := s.conn.Exec(query, contextKey, description)
	return err
}

// GetSystemQuirks retrieves all known system quirks.
func (s *Store) GetSystemQuirks() ([]SystemQuirk, error) {
	rows, err := s.conn.Query("SELECT id, context_key, description, timestamp FROM system_quirks ORDER BY timestamp DESC;")
	if err != nil {
		return nil, fmt.Errorf("failed to query quirks: %w", err)
	}
	defer rows.Close()

	var quirks []SystemQuirk
	for rows.Next() {
		var q SystemQuirk
		if err := rows.Scan(&q.ID, &q.ContextKey, &q.Description, &q.Timestamp); err != nil {
			continue
		}
		quirks = append(quirks, q)
	}
	return quirks, nil
}

// CorrectQuirk allows updating an existing quirk description if the agent's understanding changes.
func (s *Store) CorrectQuirk(id int64, newDescription string) error {
	query := `UPDATE system_quirks SET description = ?, timestamp = CURRENT_TIMESTAMP WHERE id = ?;`
	res, err := s.conn.Exec(query, newDescription, id)
	if err != nil {
		return fmt.Errorf("failed to update quirk: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no quirk found with ID %d", id)
	}
	return nil
}
