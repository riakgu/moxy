package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

type jsonUser struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	DeviceBinding string `json:"device_binding"`
	Enabled       bool   `json:"enabled"`
}

type JSONUserRepository struct {
	Log      *logrus.Logger
	FilePath string
	users    map[string]*entity.User
	mu       sync.RWMutex
}

func NewJSONUserRepository(log *logrus.Logger, filePath string) (*JSONUserRepository, error) {
	repo := &JSONUserRepository{
		Log:      log,
		FilePath: filePath,
		users:    make(map[string]*entity.User),
	}

	if err := repo.load(); err != nil {
		return nil, fmt.Errorf("load users from %s: %w", filePath, err)
	}

	log.Infof("loaded %d users from %s", len(repo.users), filePath)
	return repo, nil
}

func (r *JSONUserRepository) FindAll() ([]*entity.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	users := make([]*entity.User, 0, len(r.users))
	for _, u := range r.users {
		users = append(users, u)
	}
	return users, nil
}

func (r *JSONUserRepository) FindByUsername(username string) (*entity.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[username]
	if !ok {
		return nil, model.ErrUserNotFound
	}
	return user, nil
}

func (r *JSONUserRepository) Create(user *entity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[user.Username]; exists {
		return model.ErrUserAlreadyExists
	}

	r.users[user.Username] = user
	return r.saveLocked()
}

func (r *JSONUserRepository) Update(user *entity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[user.Username]; !exists {
		return model.ErrUserNotFound
	}

	r.users[user.Username] = user
	return r.saveLocked()
}

func (r *JSONUserRepository) Delete(username string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[username]; !exists {
		return model.ErrUserNotFound
	}

	delete(r.users, username)
	return r.saveLocked()
}

func (r *JSONUserRepository) load() error {
	data, err := os.ReadFile(r.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var users []jsonUser
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("parse %s: %w", r.FilePath, err)
	}

	for _, u := range users {
		r.users[u.Username] = &entity.User{
			Username:      u.Username,
			Password:      u.Password,
			DeviceBinding: u.DeviceBinding,
			Enabled:       u.Enabled,
		}
	}
	return nil
}

func (r *JSONUserRepository) saveLocked() error {
	users := make([]jsonUser, 0, len(r.users))
	for _, u := range r.users {
		users = append(users, jsonUser{
			Username:      u.Username,
			Password:      u.Password,
			DeviceBinding: u.DeviceBinding,
			Enabled:       u.Enabled,
		})
	}

	data, err := json.MarshalIndent(users, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal users: %w", err)
	}

	if err := os.WriteFile(r.FilePath, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", r.FilePath, err)
	}
	return nil
}
