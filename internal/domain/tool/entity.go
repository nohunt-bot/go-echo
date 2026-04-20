package tool

import "github.com/google/uuid"

type Tool struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Status      string    `json:"status"`
}

type CreateRequest struct {
	Name        string `json:"name"        validate:"required"`
	Description string `json:"description"`
	Category    string `json:"category"    validate:"required"`
	Status      string `json:"status"      validate:"required,oneof=active inactive"`
}
