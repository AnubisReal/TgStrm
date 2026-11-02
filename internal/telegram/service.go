// Package telegram implements the single-account login flow using persisted MTProto sessions.
package telegram

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gotd/td/session"
	td "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"tgstrm/internal/config"
)

const accountID = "00000000-0000-4000-8000-000000000001"

type Input struct {
	APIID    int    `json:"api_id"`
	APIHash  string `json:"api_hash"`
	Phone    string `json:"phone"`
	Code     string `json:"code"`
	Password string `json:"password"`
}
type State struct {
	Step     string `json:"step"`
	Delivery string `json:"delivery,omitempty"`
	Name     string `json:"name,omitempty"`
	UserID   string `json:"user_id,omitempty"`
}
type credentials struct {
	APIID   int
	APIHash string
}
type challenge struct {
	Phone    string
	Hash     string
	Expires  time.Time
	Step     string
	Delivery string
	NextSend time.Time
}
type Service struct {
	mu                   sync.Mutex
	DB                   *sql.DB
	Vault                *config.Store
	runtimeMu            sync.Mutex
	runtimeCancel        context.CancelFunc
	apiMu                sync.RWMutex
	runtimeAPI           *tg.Client
	runtimeClient        *td.Client
	mediaMu              sync.Mutex
	mediaPools           map[int]mediaPool
	mediaPoolRetryAfter  map[int]time.Time
	refreshTopics        chan int64
	refreshVideos        chan videoRefresh
	recoverCatalog       chan struct{}
	mirrorWake           chan struct{}
	inaccessibleChannels chan int64
	videoQueueMu         sync.Mutex
	importMu             sync.Mutex
	queuedVideos         map[videoRefresh]struct{}
	watchedTopicsMu      sync.Mutex
	watchedTopics        map[watchedTopic]watchedTopicState
	fileReferenceMu      sync.Mutex
	streamMu             sync.Mutex
	streamCache          map[string]streamCacheEntry
	streamCacheBytes     int64
	streamInflight       map[string]*streamCall
	PersistentCache      *PersistentStreamCache
	indexWarmMu          sync.Mutex
	indexWarming         map[string]struct{}
	indexPrepared        map[string]struct{}
	subMu                sync.Mutex
	subscribers          map[uint64]chan RealtimeEvent
	nextSubID            uint64
	connected            atomic.Bool
	historyReadGate      historyReadGate
}

type Profile struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Username    string `json:"username,omitempty"`
	Phone       string `json:"phone"`
	DisplayName string `json:"display_name"`
	UserID      string `json:"user_id"`
	Connected   bool   `json:"connected"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

func (s *Service) save(ctx context.Context, column, scope string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	enc, err := s.Vault.Seal(scope, raw)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "UPDATE tgstrm.telegram_accounts SET "+column+"=$1,updated_at=now() WHERE id=$2", enc, accountID)
	return err
}
func (s *Service) load(ctx context.Context, column, scope string, value any) error {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT "+column+" FROM tgstrm.telegram_accounts WHERE id=$1", accountID).Scan(&raw)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return sql.ErrNoRows
	}
	plain, err := s.Vault.Unseal(scope, raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, value)
}

type storage struct{ s *Service }

func (st storage) LoadSession(ctx context.Context) ([]byte, error) {
	var b []byte
	err := st.s.DB.QueryRowContext(ctx, "SELECT session_ciphertext FROM tgstrm.telegram_accounts WHERE id=$1", accountID).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) || len(b) == 0 && err == nil {
		return nil, session.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return st.s.Vault.Unseal("telegram/session/"+accountID, b)
}
func (st storage) StoreSession(ctx context.Context, b []byte) error {
	enc, err := st.s.Vault.Seal("telegram/session/"+accountID, b)
	if err != nil {
		return err
	}
	_, err = st.s.DB.ExecContext(ctx, "UPDATE tgstrm.telegram_accounts SET session_ciphertext=$1,updated_at=now() WHERE id=$2", enc, accountID)
	return err
}

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
var hashPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

func (s *Service) Execute(ctx context.Context, op string, in Input) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c credentials
	err := s.load(ctx, "credentials_ciphertext", "telegram/credentials", &c)
	if op == "configure" {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return State{}, err
		}
		if in.APIID <= 0 || in.APIID > 2147483647 || !hashPattern.MatchString(in.APIHash) {
			return State{}, errors.New("Revisa el API ID y el API hash de 32 caracteres.")
		}
		var status string
		statusErr := s.DB.QueryRowContext(ctx, "SELECT status FROM tgstrm.telegram_accounts WHERE id=$1", accountID).Scan(&status)
		if statusErr != nil && !errors.Is(statusErr, sql.ErrNoRows) {
			return State{}, statusErr
		}
		if status == "authorized" {
			return State{}, errors.New("Telegram ya está conectado.")
		}
		raw, err := json.Marshal(credentials{in.APIID, in.APIHash})
		if err != nil {
			return State{}, err
		}
		encrypted, err := s.Vault.Seal("telegram/credentials", raw)
		if err != nil {
			return State{}, err
		}
		_, err = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.telegram_accounts(id,credentials_ciphertext,status)
			VALUES($1,$2,'disconnected') ON CONFLICT(id) DO UPDATE SET
			credentials_ciphertext=EXCLUDED.credentials_ciphertext, session_ciphertext=NULL,
			auth_state_ciphertext=NULL, status='disconnected', updated_at=now()`, accountID, encrypted)
		return State{Step: "phone"}, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		return State{Step: "api"}, nil
	}
	if err != nil {
		return State{}, err
	}
	var pending challenge
	err = s.load(ctx, "auth_state_ciphertext", "telegram/challenge", &pending)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return State{}, err
	}
	if op == "status" && pending.Step != "" && time.Now().Before(pending.Expires) {
		return State{Step: pending.Step, Delivery: pending.Delivery}, nil
	}
	result := State{Step: "phone"}
	client := td.NewClient(c.APIID, c.APIHash, td.Options{SessionStorage: storage{s}})
	err = client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if status.Authorized {
			result = State{Step: "authorized", Name: status.User.FirstName, UserID: fmt.Sprint(status.User.ID)}
			err = s.persistProfile(ctx, client, status.User)
			return err
		}
		switch op {
		case "status":
			return nil
		case "send-code":
			if !phonePattern.MatchString(in.Phone) {
				return errors.New("Introduce un teléfono con prefijo, por ejemplo +34612345678.")
			}
			if time.Now().Before(pending.NextSend) {
				return errors.New("Espera antes de solicitar otro código.")
			}
			// Persist the rate limit before contacting Telegram: ambiguous failures must not resend automatically.
			pending = challenge{Phone: in.Phone, NextSend: time.Now().Add(time.Minute)}
			if err = s.save(ctx, "auth_state_ciphertext", "telegram/challenge", pending); err != nil {
				return err
			}
			sent, err := client.Auth().SendCode(ctx, in.Phone, auth.SendCodeOptions{})
			if err != nil {
				return err
			}
			code, ok := sent.(*tg.AuthSentCode)
			if !ok {
				return errors.New("Telegram solicita un método de acceso que aún no está soportado.")
			}
			delivery := "el método indicado por Telegram"
			switch code.Type.(type) {
			case *tg.AuthSentCodeTypeApp:
				delivery = "Telegram"
			case *tg.AuthSentCodeTypeSMS:
				delivery = "SMS"
			case *tg.AuthSentCodeTypeCall:
				delivery = "llamada"
			default:
				return errors.New("Telegram solicita un método de verificación no soportado en esta versión.")
			}
			pending.Hash = code.PhoneCodeHash
			pending.Step = "code"
			pending.Delivery = delivery
			pending.Expires = time.Now().Add(10 * time.Minute)
			if code.Timeout > 60 {
				pending.NextSend = time.Now().Add(time.Duration(code.Timeout) * time.Second)
			}
			err = s.save(ctx, "auth_state_ciphertext", "telegram/challenge", pending)
			result = State{Step: "code", Delivery: delivery}
			return err
		case "verify-code", "verify-password":
			expected := "code"
			if op == "verify-password" {
				expected = "password"
			}
			if pending.Step != expected || time.Now().After(pending.Expires) {
				return errors.New("La verificación ha caducado. Solicita un nuevo código.")
			}
			var authorization *tg.AuthAuthorization
			if op == "verify-code" {
				if len(in.Code) < 3 || len(in.Code) > 12 {
					return errors.New("Revisa el código de verificación.")
				}
				authorization, err = client.Auth().SignIn(ctx, pending.Phone, in.Code, pending.Hash)
			} else {
				if in.Password == "" {
					return errors.New("Introduce tu contraseña de verificación en dos pasos.")
				}
				password := []byte(in.Password)
				authorization, err = client.Auth().PasswordWith(ctx, auth.PasswordHashFor(password))
				for i := range password {
					password[i] = 0
				}
			}
			if errors.Is(err, auth.ErrPasswordAuthNeeded) {
				pending.Step = "password"
				err = s.save(ctx, "auth_state_ciphertext", "telegram/challenge", pending)
				result = State{Step: "password"}
				return err
			}
			if err != nil {
				return err
			}
			user, ok := authorization.User.(*tg.User)
			if !ok {
				return errors.New("No se pudo identificar la cuenta.")
			}
			result = State{Step: "authorized", Name: user.FirstName, UserID: fmt.Sprint(user.ID)}
			err = s.persistProfile(ctx, client, user)
			return err
		default:
			return errors.New("Operación no válida.")
		}
	})
	if err == nil && result.Step == "authorized" {
		s.Start(context.Background())
	}
	return result, err
}

func (s *Service) persistProfile(ctx context.Context, client *td.Client, user *tg.User) error {
	profile := Profile{FirstName: user.FirstName, LastName: user.LastName, Username: user.Username, Phone: user.Phone, UserID: fmt.Sprint(user.ID)}
	if profile.Phone != "" && profile.Phone[0] != '+' {
		profile.Phone = "+" + profile.Phone
	}
	profile.DisplayName = user.FirstName
	if user.LastName != "" {
		profile.DisplayName += " " + user.LastName
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	sealed, err := s.Vault.Seal("telegram/profile/"+accountID, raw)
	if err != nil {
		return err
	}
	var avatar []byte
	photos, photoErr := client.API().PhotosGetUserPhotos(ctx, &tg.PhotosGetUserPhotosRequest{UserID: &tg.InputUserSelf{}, Limit: 1})
	if photoErr == nil {
		items := photos.GetPhotos()
		if len(items) > 0 {
			if photo, ok := items[0].AsNotEmpty(); ok {
				for i := len(photo.Sizes) - 1; i >= 0 && len(avatar) == 0; i-- {
					var output bytes.Buffer
					thumb := photo.Sizes[i].GetType()
					if _, downloadErr := client.Download(photo.AsInputPhotoFileLocation(thumb)).Stream(ctx, &output); downloadErr == nil && output.Len() <= 2<<20 {
						avatar = output.Bytes()
					}
				}
			}
		}
	}
	_, err = s.DB.ExecContext(ctx, `WITH previous AS (
		SELECT status FROM tgstrm.telegram_accounts WHERE id=$4 FOR UPDATE
	), updated AS (
		UPDATE tgstrm.telegram_accounts SET status='authorized',telegram_user_id=$1,
		profile_ciphertext=$2,avatar=COALESCE(NULLIF($3::bytea,''::bytea),avatar),
		avatar_content_type=CASE WHEN octet_length($3::bytea)>0 THEN 'image/jpeg' ELSE avatar_content_type END,
		auth_state_ciphertext=NULL,last_seen_at=now(),updated_at=now() WHERE id=$4 RETURNING id
	) INSERT INTO tgstrm.activity_events(kind,status,title,detail)
	SELECT 'telegram_session','completed','Telegram conectado','La sesión se autorizó correctamente y los trabajos pendientes se reanudarán.'
	FROM previous,updated WHERE previous.status<>'authorized'`, user.ID, sealed, avatar, accountID)
	return err
}

// Start keeps one MTProto client alive and reconnects it after transient failures.
func (s *Service) Start(parent context.Context) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.runtimeCancel != nil {
		return
	}
	var status string
	if err := s.DB.QueryRow("SELECT status FROM tgstrm.telegram_accounts WHERE id=$1", accountID).Scan(&status); err != nil || status != "authorized" {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.runtimeCancel = cancel
	go s.run(ctx)
}

func (s *Service) Stop() {
	s.runtimeMu.Lock()
	if s.runtimeCancel != nil {
		s.runtimeCancel()
		s.runtimeCancel = nil
	}
	s.runtimeMu.Unlock()
}

// Connected reports whether the long-lived authorized MTProto client is
// currently available. Durable workers use this as a pause gate so a logout or
// revoked authorization never turns queued work into a failure.
func (s *Service) Connected() bool { return s.connected.Load() }

// Logout revokes only this Telegram authorization. Catalog, settings, mirror
// rules and durable queues deliberately remain in PostgreSQL for the next
// authorization.
func (s *Service) Logout(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("telegram is reconnecting")
	}
	if _, err := api.AuthLogOut(ctx); err != nil && !auth.IsUnauthorized(err) {
		return err
	}
	if err := s.clearAuthorization(ctx, "disconnected", "Sesión de Telegram cerrada", "La autorización se cerró correctamente; el catálogo y los trabajos pendientes se conservaron."); err != nil {
		return err
	}
	s.Stop()
	s.connected.Store(false)
	return nil
}

func (s *Service) clearAuthorization(ctx context.Context, status, title, detail string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE tgstrm.telegram_accounts SET status=$2,telegram_user_id=NULL,
		session_ciphertext=NULL,auth_state_ciphertext=NULL,profile_ciphertext=NULL,avatar=NULL,avatar_content_type=NULL,
		connected_at=NULL,updated_at=now() WHERE id=$1 AND (status<>$2 OR session_ciphertext IS NOT NULL)`, accountID, status)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.activity_events(kind,status,title,detail)
			VALUES('telegram_session',$1,$2,$3)`, map[bool]string{true: "error", false: "completed"}[status == "revoked"], title, detail); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) run(ctx context.Context) {
	for ctx.Err() == nil {
		var c credentials
		if err := s.load(ctx, "credentials_ciphertext", "telegram/credentials", &c); err != nil {
			return
		}
		dispatcher := s.updateDispatcher()
		stateStore := updateStorage{s: s}
		manager := updates.New(updates.Config{
			Handler:      dispatcher,
			Storage:      stateStore,
			AccessHasher: stateStore,
			OnChannelTooLong: func(channelID int64) {
				s.queueChannelRecovery(channelID)
			},
			OnChannelInaccessible:           func(channelID int64) { s.queueInaccessibleChannel(channelID) },
			OnTooLong:                       func() { s.queueCatalogRecovery() },
			OnLoadUserStateFailed:           func() { s.queueCatalogRecovery() },
			OnLoadChannelStateFailed:        func(channelID int64) { s.queueChannelRecovery(channelID) },
			MaxChannelDifferenceConcurrency: 4,
		})
		client := td.NewClient(c.APIID, c.APIHash, td.Options{SessionStorage: storage{s}, UpdateHandler: manager})
		err := client.Run(ctx, func(runCtx context.Context) error {
			status, err := client.Auth().Status(runCtx)
			if err != nil {
				return err
			}
			if !status.Authorized {
				_ = s.clearAuthorization(runCtx, "revoked", "Telegram necesita volver a vincularse", "Telegram revocó o invalidó esta autorización. Los trabajos permanecen guardados y se reanudarán al iniciar sesión.")
				return errors.New("telegram session revoked")
			}
			if err = s.persistProfile(runCtx, client, status.User); err != nil {
				return err
			}
			s.apiMu.Lock()
			s.runtimeAPI = client.API()
			s.runtimeClient = client
			s.apiMu.Unlock()
			defer func() {
				s.apiMu.Lock()
				s.runtimeAPI = nil
				s.runtimeClient = nil
				s.apiMu.Unlock()
				s.closeMediaPools(client)
			}()
			_ = s.syncDialogs(runCtx, client.API())
			s.runtimeMu.Lock()
			if s.refreshTopics == nil {
				s.refreshTopics = make(chan int64, 128)
			}
			if s.refreshVideos == nil {
				s.refreshVideos = make(chan videoRefresh, 256)
			}
			if s.recoverCatalog == nil {
				s.recoverCatalog = make(chan struct{}, 1)
			}
			if s.mirrorWake == nil {
				s.mirrorWake = make(chan struct{}, 1)
			}
			if s.inaccessibleChannels == nil {
				s.inaccessibleChannels = make(chan int64, 64)
			}
			refreshTopics := s.refreshTopics
			refreshVideos := s.refreshVideos
			recoverCatalog := s.recoverCatalog
			mirrorWake := s.mirrorWake
			inaccessibleChannels := s.inaccessibleChannels
			s.runtimeMu.Unlock()
			s.connected.Store(true)
			_, _ = s.DB.ExecContext(runCtx, "UPDATE tgstrm.telegram_accounts SET connected_at=now(),last_seen_at=now() WHERE id=$1", accountID)
			go s.warmStreamRoute(runCtx)
			// Catalog imports are resumed by the API's leased PostgreSQL worker.
			// Starting a second recovery loop here could resend the same manifest
			// concurrently with that worker after reconnect or process restart.
			go func() {
				recoveryCtx, cancel := context.WithTimeout(runCtx, 5*time.Minute)
				if err := s.RecoverMirrorSources(recoveryCtx); err != nil && recoveryCtx.Err() == nil {
					log.Printf("mirror startup reconciliation: %v", err)
				}
				cancel()
				s.ProcessMirrorDeliveries(runCtx)
			}()
			managerDone := make(chan error, 1)
			go func() {
				managerDone <- manager.Run(runCtx, client.API(), status.User.ID, updates.AuthOptions{IsBot: status.User.Bot})
			}()
			dialogTicker := time.NewTicker(5 * time.Minute)
			primaryTopicsTicker := time.NewTicker(10 * time.Second)
			var nextUnresolvedCheck time.Time
			outboxTicker := time.NewTicker(300 * time.Millisecond)
			mirrorTicker := time.NewTicker(15 * time.Second)
			defer dialogTicker.Stop()
			defer primaryTopicsTicker.Stop()
			defer outboxTicker.Stop()
			defer mirrorTicker.Stop()
			for {
				select {
				case <-runCtx.Done():
					return runCtx.Err()
				case err := <-managerDone:
					return err
				case channelID := <-refreshTopics:
					refreshCtx, cancel := context.WithTimeout(runCtx, 30*time.Second)
					_ = s.refreshTopicsForChannel(refreshCtx, channelID)
					cancel()
				case refresh := <-refreshVideos:
					s.videoQueueMu.Lock()
					delete(s.queuedVideos, refresh)
					s.videoQueueMu.Unlock()
					refreshCtx, cancel := context.WithTimeout(runCtx, 30*time.Second)
					_ = s.refreshVideosForTopic(refreshCtx, refresh)
					cancel()
				case <-recoverCatalog:
					recoveryCtx, cancel := context.WithTimeout(runCtx, 10*time.Minute)
					_ = s.recoverCatalogState(recoveryCtx, client.API())
					cancel()
				case <-mirrorWake:
					go s.ProcessMirrorDeliveries(runCtx)
				case <-mirrorTicker.C:
					go s.ProcessMirrorDeliveries(runCtx)
				case channelID := <-inaccessibleChannels:
					_ = s.markChannelInaccessible(runCtx, channelID)
				case <-outboxTicker.C:
					_ = s.deliverOutbox(runCtx)
				case <-dialogTicker.C:
					_ = s.syncDialogs(runCtx, client.API())
				case <-primaryTopicsTicker.C:
					// MTProto updates remain the fast path. This small authoritative
					// reconciliation closes gaps caused by a transient disconnect.
					var primaryGroupID string
					if s.DB.QueryRowContext(runCtx, `SELECT c.id::text FROM tgstrm.managed_catalog_chats c
 LEFT JOIN tgstrm.sync_states ss ON ss.account_id=c.account_id AND ss.scope='topics' AND ss.resource_id=c.id
 WHERE c.is_active AND c.is_forum ORDER BY ss.completed_at NULLS FIRST,c.id LIMIT 1`).Scan(&primaryGroupID) == nil {
						refreshCtx, cancel := context.WithTimeout(runCtx, 20*time.Second)
						if err := s.RefreshTopics(refreshCtx, primaryGroupID); err != nil {
							log.Printf("primary topic reconciliation: %v", err)
						}
						cancel()
						if time.Now().After(nextUnresolvedCheck) {
							// Caption edits are handled by updates; this is only the
							// fallback for gaps, not a ten-second retry of unresolved files.
							nextUnresolvedCheck = time.Now().Add(time.Minute)
							unresolvedCtx, unresolvedCancel := context.WithTimeout(runCtx, 30*time.Second)
							if err := s.reconcileOneUnresolvedPrimaryTopic(unresolvedCtx); err != nil {
								if wait, ok := tgerr.AsFloodWait(err); ok && wait > time.Minute {
									nextUnresolvedCheck = time.Now().Add(wait)
								}
								log.Printf("unresolved video reconciliation: %v", err)
							}
							unresolvedCancel()
						}
						watchedCtx, watchedCancel := context.WithTimeout(runCtx, 30*time.Second)
						if err := s.reconcileOneWatchedTopic(watchedCtx); err != nil {
							log.Printf("watched topic reconciliation: %v", err)
						}
						watchedCancel()
					}
				}
			}
		})
		s.connected.Store(false)
		if ctx.Err() != nil {
			return
		}
		if auth.IsUnauthorized(err) {
			_ = s.clearAuthorization(context.Background(), "revoked", "Telegram necesita volver a vincularse", "Telegram revocó o invalidó esta autorización. Los trabajos permanecen guardados y se reanudarán al iniciar sesión.")
			return
		}
		_ = err
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) Profile(ctx context.Context) (Profile, error) {
	var encrypted []byte
	var avatar []byte
	err := s.DB.QueryRowContext(ctx, "SELECT profile_ciphertext,avatar FROM tgstrm.telegram_accounts WHERE id=$1 AND status='authorized'", accountID).Scan(&encrypted, &avatar)
	if err != nil {
		return Profile{}, err
	}
	plain, err := s.Vault.Unseal("telegram/profile/"+accountID, encrypted)
	if err != nil {
		return Profile{}, err
	}
	var profile Profile
	if err = json.Unmarshal(plain, &profile); err != nil {
		return Profile{}, err
	}
	profile.Connected = s.connected.Load()
	if len(avatar) > 0 {
		profile.AvatarURL = "/api/v1/profile/avatar"
	}
	return profile, nil
}

func (s *Service) Avatar(ctx context.Context) ([]byte, string, error) {
	var content []byte
	var contentType sql.NullString
	err := s.DB.QueryRowContext(ctx, "SELECT avatar,avatar_content_type FROM tgstrm.telegram_accounts WHERE id=$1", accountID).Scan(&content, &contentType)
	return content, contentType.String, err
}

// PublicError avoids leaking RPC payloads, phone numbers or connection credentials.
func PublicError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Telegram no respondió a tiempo. Comprueba el estado antes de reenviar el código."
	}
	if errors.Is(err, auth.ErrPasswordInvalid) {
		return "La contraseña de verificación en dos pasos no es correcta."
	}
	if tgerr.Is(err, "PHONE_CODE_INVALID") {
		return "El código no es correcto."
	}
	if tgerr.Is(err, "PHONE_CODE_EXPIRED") {
		return "El código ha caducado. Solicita otro código."
	}
	if tgerr.Is(err, "API_ID_INVALID") {
		return "Telegram ha rechazado el API ID o el API hash."
	}
	if tgerr.Is(err, "PHONE_NUMBER_INVALID") {
		return "Telegram no reconoce ese número de teléfono."
	}
	if wait, ok := tgerr.AsFloodWait(err); ok {
		return fmt.Sprintf("Telegram pide esperar %d segundos antes de reintentar.", int(wait.Seconds()))
	}
	for _, message := range []string{"Telegram ya está conectado.", "Revisa el API ID y el API hash de 32 caracteres.", "Introduce un teléfono con prefijo, por ejemplo +34612345678.", "Espera antes de solicitar otro código.", "La verificación ha caducado. Solicita un nuevo código.", "Revisa el código de verificación.", "Introduce tu contraseña de verificación en dos pasos.", "Telegram solicita un método de acceso que aún no está soportado.", "Telegram solicita un método de verificación no soportado en esta versión."} {
		if err.Error() == message {
			return message
		}
	}
	return "No se pudo completar el acceso. Comprueba la conexión y los datos de Telegram."
}
