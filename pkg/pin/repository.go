package pin

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository errors.
var (
	// ErrSecurityQuestionNotFound is returned when no security questions
	// exist for the requested user.
	ErrSecurityQuestionNotFound = errors.New("security questions not found for user")
)

// SecurityQuestionRepository provides data access for the security_questions
// table. It is safe for concurrent use because each method scopes its own
// GORM session via WithContext.
type SecurityQuestionRepository struct {
	db *gorm.DB
}

// NewSecurityQuestionRepository creates a new SecurityQuestionRepository backed
// by the provided GORM connection. The caller must ensure db is non-nil.
func NewSecurityQuestionRepository(db *gorm.DB) *SecurityQuestionRepository {
	return &SecurityQuestionRepository{db: db}
}

// GetByUserID returns all security questions for the given user, ordered by
// question_id. Returns ErrSecurityQuestionNotFound if no rows exist.
func (r *SecurityQuestionRepository) GetByUserID(ctx context.Context, userID string) ([]models.SecurityQuestion, error) {
	var questions []models.SecurityQuestion
	result := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("question_id ASC").
		Find(&questions)
	if result.Error != nil {
		log.Printf("SecurityQuestionRepository.GetByUserID: database error: %v", result.Error)
		return nil, fmt.Errorf("get security questions for user %s: %w", userID, result.Error)
	}
	if len(questions) == 0 {
		return nil, ErrSecurityQuestionNotFound
	}
	return questions, nil
}

// UpsertForUser inserts or updates security questions for a user. Each
// question is upserted on the (user_id, question_id) unique constraint,
// replacing the answer_hash if the row already exists. The operation runs
// inside a transaction so that all questions succeed or none do.
func (r *SecurityQuestionRepository) UpsertForUser(ctx context.Context, questions []models.SecurityQuestion) error {
	if len(questions) == 0 {
		return nil
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range questions {
			result := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "user_id"}, {Name: "question_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"answer_hash", "updated_at"}),
			}).Create(&questions[i])
			if result.Error != nil {
				return fmt.Errorf("upsert question %d for user %s: %w",
					questions[i].QuestionID, questions[i].UserID, result.Error)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("upsert security questions: %w", err)
	}
	return nil
}

// GetQuestionIDsByUserID returns the question IDs that the user has configured,
// in ascending order. This is used to prompt the user with their own questions
// during recovery without exposing answer hashes.
func (r *SecurityQuestionRepository) GetQuestionIDsByUserID(ctx context.Context, userID string) ([]int, error) {
	var ids []int
	result := r.db.WithContext(ctx).
		Model(&models.SecurityQuestion{}).
		Where("user_id = ?", userID).
		Order("question_id ASC").
		Pluck("question_id", &ids)
	if result.Error != nil {
		return nil, fmt.Errorf("get question IDs for user %s: %w", userID, result.Error)
	}
	return ids, nil
}

// DeleteByUserID removes all security questions for the given user. This is
// typically called when an account is being deactivated.
func (r *SecurityQuestionRepository) DeleteByUserID(ctx context.Context, userID string) error {
	result := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&models.SecurityQuestion{})
	if result.Error != nil {
		return fmt.Errorf("delete security questions for user %s: %w", userID, result.Error)
	}
	return nil
}
