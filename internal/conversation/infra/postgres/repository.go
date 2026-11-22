// Package postgres — реализация репозитория контекста conversation на sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/conversation/domain"
	"closerai/internal/conversation/infra/postgres/sqlcgen"
	"closerai/internal/kernel"
	"closerai/internal/platform/db"
)

type Repository struct {
	pool *pgxpool.Pool
	tx   *db.TxManager
}

var _ domain.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, tx: db.NewTxManager(pool)}
}

func (r *Repository) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(db.Executor(ctx, r.pool))
}

func (r *Repository) UpsertCustomer(ctx context.Context, c domain.Customer) (domain.Customer, error) {
	row, err := r.q(ctx).UpsertCustomer(ctx, sqlcgen.UpsertCustomerParams{
		TenantID:  c.TenantID,
		TgUserID:  c.TgUserID,
		Username:  c.Username,
		FirstName: c.FirstName,
		LastName:  c.LastName,
	})
	if err != nil {
		return domain.Customer{}, fmt.Errorf("conversation: upsert customer: %w", err)
	}
	return domain.Customer{
		ID: row.ID, TenantID: row.TenantID, TgUserID: row.TgUserID,
		Username: row.Username, FirstName: row.FirstName, LastName: row.LastName,
	}, nil
}

func (r *Repository) OpenConversation(ctx context.Context, tenantID, botID, customerID uuid.UUID) (domain.Conversation, error) {
	var conv domain.Conversation
	// Блокировка FOR UPDATE имеет смысл только в транзакции; если вызывающий
	// её не открыл, открываем свою (вложенный Do переиспользует внешнюю).
	err := r.tx.Do(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		key := sqlcgen.LockOpenConversationParams{TenantID: tenantID, BotID: botID, CustomerID: customerID}
		row, err := q.LockOpenConversation(ctx, key)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err = q.InsertConversation(ctx, sqlcgen.InsertConversationParams(key)); err != nil {
				return fmt.Errorf("conversation: insert: %w", err)
			}
			// Вставили мы или параллельная транзакция (тогда ждём её коммита
			// на блокировке) — диалог теперь есть.
			row, err = q.LockOpenConversation(ctx, key)
		}
		if err != nil {
			return fmt.Errorf("conversation: open: %w", err)
		}
		conv = conversationFromRow(sqlcgen.GetConversationRow(row))
		return nil
	})
	return conv, err
}

func (r *Repository) GetConversation(ctx context.Context, tenantID, id uuid.UUID) (domain.Conversation, error) {
	row, err := r.q(ctx).GetConversation(ctx, sqlcgen.GetConversationParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Conversation{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("conversation: get: %w", err)
	}
	return conversationFromRow(row), nil
}

func (r *Repository) AddMessage(ctx context.Context, tenantID uuid.UUID, m domain.Message) (domain.Message, bool, error) {
	var (
		saved domain.Message
		added bool
	)
	err := r.tx.Do(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		row, err := q.InsertMessage(ctx, sqlcgen.InsertMessageParams{
			ConversationID: m.ConversationID,
			Sender:         string(m.Sender),
			UserID:         m.UserID,
			Text:           m.Text,
			TgMessageID:    pgtype.Int8{Int64: m.TgMessageID, Valid: m.TgMessageID != 0},
			CreatedAt:      m.CreatedAt,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // повтор: такое сообщение Telegram уже сохранено
		}
		if err != nil {
			return fmt.Errorf("conversation: insert message: %w", err)
		}
		if err := q.TouchConversation(ctx, sqlcgen.TouchConversationParams{
			TenantID: tenantID, ID: m.ConversationID, LastMessageAt: row.CreatedAt,
		}); err != nil {
			return fmt.Errorf("conversation: touch: %w", err)
		}
		saved, added = messageFromRow(row), true
		return nil
	})
	return saved, added, err
}

func (r *Repository) GetMessage(ctx context.Context, tenantID, conversationID uuid.UUID, id int64) (domain.Message, error) {
	row, err := r.q(ctx).GetMessage(ctx, sqlcgen.GetMessageParams{
		TenantID: tenantID, ConversationID: conversationID, ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Message{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Message{}, fmt.Errorf("conversation: get message: %w", err)
	}
	return messageFromRow(row), nil
}

func conversationFromRow(row sqlcgen.GetConversationRow) domain.Conversation {
	return domain.Conversation{
		ID:             row.ID,
		TenantID:       row.TenantID,
		BotID:          row.BotID,
		CustomerID:     row.CustomerID,
		ChatID:         row.ChatID,
		Status:         kernel.ConversationStatus(row.Status),
		AssignedUserID: row.AssignedUserID,
		LastMessageAt:  row.LastMessageAt,
		CreatedAt:      row.CreatedAt,
		ClosedAt:       row.ClosedAt,
	}
}

func messageFromRow(row sqlcgen.Message) domain.Message {
	return domain.Message{
		ID:             row.ID,
		ConversationID: row.ConversationID,
		Sender:         kernel.Sender(row.Sender),
		UserID:         row.UserID,
		Text:           row.Text,
		TgMessageID:    row.TgMessageID.Int64,
		CreatedAt:      row.CreatedAt,
	}
}
