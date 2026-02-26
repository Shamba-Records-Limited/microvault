package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SecurityQuestion stores a hashed answer to a predefined security question
// for a given user. Each user may have multiple security questions, identified
// by QuestionID (an integer index into a predefined question list). The
// combination of (UserID, QuestionID) is unique.
type SecurityQuestion struct {
	ID         string    `json:"id" gorm:"type:uuid;primaryKey"`
	UserID     string    `json:"user_id" gorm:"type:uuid;not null;index"`
	QuestionID int       `json:"question_id" gorm:"not null"`
	AnswerHash string    `json:"-" gorm:"type:varchar(72);not null"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime;not null"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime;not null"`

	User User `gorm:"foreignKey:UserID"`
}

// TableName specifies the table name for the SecurityQuestion model.
func (SecurityQuestion) TableName() string {
	return "security_questions"
}

// BeforeCreate sets a UUIDv7 primary key before inserting a new row.
func (sq *SecurityQuestion) BeforeCreate(tx *gorm.DB) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	sq.ID = id.String()
	return nil
}

// PredefinedQuestionCount is the total number of predefined security questions
// available for selection. Question IDs range from 1 to PredefinedQuestionCount.
const PredefinedQuestionCount = 5
