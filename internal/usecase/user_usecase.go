package usecase

import (
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
)

type UserRepository interface {
	FindAll() ([]*entity.User, error)
	FindByUsername(username string) (*entity.User, error)
	Create(user *entity.User) error
	Update(user *entity.User) error
	Delete(username string) error
}

type UserUseCase struct {
	Log  *logrus.Logger
	Repo UserRepository
}

func NewUserUseCase(log *logrus.Logger, repo UserRepository) *UserUseCase {
	return &UserUseCase{
		Log:  log,
		Repo: repo,
	}
}

func (c *UserUseCase) List() ([]model.UserResponse, error) {
	users, err := c.Repo.FindAll()
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	result := make([]model.UserResponse, 0, len(users))
	for _, u := range users {
		result = append(result, *converter.UserToResponse(u))
	}
	return result, nil
}

func (c *UserUseCase) GetByUsername(username string) (*model.UserResponse, error) {
	user, err := c.Repo.FindByUsername(username)
	if err != nil {
		return nil, err
	}
	return converter.UserToResponse(user), nil
}

func (c *UserUseCase) Create(req *model.CreateUserRequest) (*model.UserResponse, error) {
	existing, _ := c.Repo.FindByUsername(req.Username)
	if existing != nil {
		return nil, model.ErrUserAlreadyExists
	}

	user := &entity.User{
		Username:      req.Username,
		Password:      req.Password,
		DeviceBinding: req.DeviceBinding,
		Enabled:       req.Enabled,
	}

	if err := c.Repo.Create(user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	if c.Log != nil {
		c.Log.Infof("user created: %s", req.Username)
	}
	return converter.UserToResponse(user), nil
}

func (c *UserUseCase) Update(req *model.UpdateUserRequest) (*model.UserResponse, error) {
	user, err := c.Repo.FindByUsername(req.Username)
	if err != nil {
		return nil, err
	}

	if req.Password != "" {
		user.Password = req.Password
	}
	if req.Enabled != nil {
		user.Enabled = *req.Enabled
	}
	user.DeviceBinding = req.DeviceBinding

	if err := c.Repo.Update(user); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}

	if c.Log != nil {
		c.Log.Infof("user updated: %s", req.Username)
	}
	return converter.UserToResponse(user), nil
}

func (c *UserUseCase) Delete(username string) error {
	if _, err := c.Repo.FindByUsername(username); err != nil {
		return err
	}

	if err := c.Repo.Delete(username); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	if c.Log != nil {
		c.Log.Infof("user deleted: %s", username)
	}
	return nil
}
