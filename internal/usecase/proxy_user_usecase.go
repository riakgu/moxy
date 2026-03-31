package usecase

import (
	"database/sql"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type ProxyUserUseCase struct {
	Log  *logrus.Logger
	DB   *sql.DB
	Repo *repository.ProxyUserRepository
}

func NewProxyUserUseCase(log *logrus.Logger, db *sql.DB, repo *repository.ProxyUserRepository) *ProxyUserUseCase {
	return &ProxyUserUseCase{Log: log, DB: db, Repo: repo}
}

func (c *ProxyUserUseCase) List() ([]model.ProxyUserResponse, error) {
	users, err := c.Repo.FindAll(c.DB)
	if err != nil {
		return nil, err
	}
	var result []model.ProxyUserResponse
	for _, u := range users {
		result = append(result, *converter.ProxyUserToResponse(u))
	}
	return result, nil
}

func (c *ProxyUserUseCase) Create(req *model.CreateProxyUserRequest) (*model.ProxyUserResponse, error) {
	hash, err := repository.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	user := &entity.ProxyUser{
		ID:            uuid.NewString(),
		Username:      req.Username,
		PasswordHash:  hash,
		DeviceBinding: req.DeviceBinding,
		Enabled:       true,
	}
	if err := c.Repo.Create(c.DB, user); err != nil {
		return nil, err
	}
	return converter.ProxyUserToResponse(user), nil
}

func (c *ProxyUserUseCase) GetByUsername(username string) (*model.ProxyUserResponse, error) {
	user, err := c.Repo.FindByUsername(c.DB, username)
	if err != nil {
		return nil, err
	}
	return converter.ProxyUserToResponse(user), nil
}

func (c *ProxyUserUseCase) Update(req *model.UpdateProxyUserRequest) (*model.ProxyUserResponse, error) {
	user, err := c.Repo.FindByUsername(c.DB, req.Username)
	if err != nil {
		return nil, err
	}
	if req.Password != "" {
		hash, err := repository.HashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		user.PasswordHash = hash
	}
	if req.DeviceBinding != "" {
		user.DeviceBinding = req.DeviceBinding
	}
	if req.Enabled != nil {
		user.Enabled = *req.Enabled
	}
	if err := c.Repo.Update(c.DB, user); err != nil {
		return nil, err
	}
	return converter.ProxyUserToResponse(user), nil
}

func (c *ProxyUserUseCase) Delete(username string) error {
	return c.Repo.Delete(c.DB, username)
}
