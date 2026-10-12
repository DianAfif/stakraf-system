package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"
	"strings"
)

func resetPassword(ctx context.Context, db *sql.DB, email string, input io.Reader) error {
	line, err := bufio.NewReader(io.LimitReader(input, 74)).ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	password := strings.TrimRight(line, "\r\n")
	if len(password) < 12 || len(password) > 72 {
		return fmt.Errorf("password must contain 12–72 bytes")
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var uid string
	if err = tx.QueryRowContext(ctx, "UPDATE users SET password_hash=$1 WHERE email=$2 RETURNING id", string(digest), strings.ToLower(strings.TrimSpace(email))).Scan(&uid); err != nil {
		return fmt.Errorf("account not found or database unavailable")
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=$1", uid); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,user_id,event_type,resource_type,resource_id,details) VALUES($1,$2,'account.password_recovered','user',$2,'Reset by console host administrator')`, id(), uid); err != nil {
		return err
	}
	return tx.Commit()
}
