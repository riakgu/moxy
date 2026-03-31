package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"

	"github.com/riakgu/moxy/internal/entity"
)

type ProxyUserRepository struct {
	Log *logrus.Logger
}

func NewProxyUserRepository(log *logrus.Logger) *ProxyUserRepository {
	return &ProxyUserRepository{Log: log}
}

func (r *ProxyUserRepository) Create(db *sql.DB, user *entity.ProxyUser) error {
	now := time.Now().UnixMilli()
	user.CreatedAt = now
	user.UpdatedAt = now
	_, err := db.Exec(
		`INSERT INTO proxy_users (id, username, password_hash, device_binding, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.PasswordHash, user.DeviceBinding,
		user.Enabled, user.CreatedAt, user.UpdatedAt,
	)
	return err
}

func (r *ProxyUserRepository) FindAll(db *sql.DB) ([]*entity.ProxyUser, error) {
	rows, err := db.Query("SELECT id, username, password_hash, device_binding, enabled, created_at, updated_at FROM proxy_users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*entity.ProxyUser
	for rows.Next() {
		u := &entity.ProxyUser{}
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DeviceBinding,
			&u.Enabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *ProxyUserRepository) FindByUsername(db *sql.DB, username string) (*entity.ProxyUser, error) {
	u := &entity.ProxyUser{}
	err := db.QueryRow(
		"SELECT id, username, password_hash, device_binding, enabled, created_at, updated_at FROM proxy_users WHERE username = ?", username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DeviceBinding,
		&u.Enabled, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("proxy user not found: %s", username)
	}
	return u, err
}

func (r *ProxyUserRepository) Update(db *sql.DB, user *entity.ProxyUser) error {
	user.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(
		`UPDATE proxy_users SET password_hash=?, device_binding=?, enabled=?, updated_at=? WHERE id=?`,
		user.PasswordHash, user.DeviceBinding, user.Enabled, user.UpdatedAt, user.ID,
	)
	return err
}

func (r *ProxyUserRepository) Delete(db *sql.DB, username string) error {
	_, err := db.Exec("DELETE FROM proxy_users WHERE username = ?", username)
	return err
}

// VerifyPassword checks a plaintext password against the stored bcrypt hash
func (r *ProxyUserRepository) VerifyPassword(hash string, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HashPassword creates a bcrypt hash from a plaintext password
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}
