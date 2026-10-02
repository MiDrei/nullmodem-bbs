// Package web implements two REST APIs sharing one daemon and one
// built SvelteKit SPA (see web/src/routes/admin and
// web/src/routes/(portal)): a JWT-authenticated sysop admin API
// (BBS configuration editing, area/user management, the ANSI screen
// designer) under /api/..., and a JWT-authenticated BBS user portal
// API (reading/posting echomail, netmail, file browsing/download)
// under /api/bbs/..., open to any registered account rather than
// sysop-only (see requireAuth vs requireBBSUser in auth.go).
package web

import (
	"database/sql"
	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/community"
	"git.maik.ch/nullmodem/bbs/internal/guard"
	"git.maik.ch/nullmodem/bbs/internal/nodelist"
	"git.maik.ch/nullmodem/bbs/internal/push"
	"net/http"
	"os"
	"path/filepath"

	"git.maik.ch/nullmodem/bbs/internal/applog"
	"git.maik.ch/nullmodem/bbs/internal/archive"
	"git.maik.ch/nullmodem/bbs/internal/areafix"
	"git.maik.ch/nullmodem/bbs/internal/binkplog"
	"git.maik.ch/nullmodem/bbs/internal/file"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/services"
	"git.maik.ch/nullmodem/bbs/internal/session"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// Server holds the dependencies shared by all admin API handlers.
type Server struct {
	Users       *user.Store
	Messages    *message.Store
	Files       *file.Store
	Netmail     *netmail.Store
	Nodes       *session.Store
	Logs        *applog.Store
	Logger      *applog.Logger
	EchoAreafix *areafix.EchoStore
	FileAreafix *areafix.FileStore
	Archive     *archive.Store
	BinkpLog    *binkplog.Store
	// Services is the daemons' registry (see internal/services) -- the
	// Services page, and where a saved change that needs a restart is
	// recorded. May be nil in tests.
	Services      *services.Store
	BBSConfigPath string
	// WebConfigPath is web.yaml's, for the backup.
	WebConfigPath string
	// FTNAddress is this system's own primary FTN address (see
	// config.Config.PrimaryFTNAddress), stamped on netmail the BBS
	// portal's caller composes -- same one internal/bbs's own Server
	// uses, cached at startup the same way (a config change via the
	// admin Settings page needs a daemon restart to take effect here,
	// consistent with how the rest of this project already treats
	// config reloading).
	FTNAddress string
	JWTSecret  []byte
	StaticDir  string
	// DB and DBPath are for the maintenance page (internal/maintenance),
	// which works on the database directly. May be nil in tests.
	DB     *sql.DB
	DBPath string
	// Push sends the mobile reader's notifications; nil turns them off.
	Push *push.Sender
	// Guard locks out addresses that keep failing to log in; nil off.
	Guard *guard.Guard
	// Chat holds the chat rooms and one-liners; nil off.
	Chat *chat.Store
	// Nodelist and Community: the nodelists, polls and BBS list.
	Nodelist  *nodelist.Store
	Community *community.Store
	// TerminalAddr is the bbs daemon's Telnet port for the web
	// terminal (terminal_handler.go); empty turns it off.
	TerminalAddr string
}

// logInfo/logWarn are nil-safe wrappers around Server.Logger, which is
// optional (e.g. in tests that don't care about activity logging).
func (s *Server) logInfo(format string, args ...any) {
	if s.Logger != nil {
		s.Logger.Info(format, args...)
	}
}

func (s *Server) logWarn(format string, args ...any) {
	if s.Logger != nil {
		s.Logger.Warn(format, args...)
	}
}

// Routes builds the HTTP handler for the admin API and static UI.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/bbs/info", s.handleBBSInfo)
	mux.HandleFunc("GET /api/terminal", s.handleTerminal)
	mux.HandleFunc("GET /api/bbs/welcome-screen", s.handleWelcomeScreen)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.Handle("GET /api/config", s.requireAuth(http.HandlerFunc(s.handleGetConfig)))
	mux.Handle("PUT /api/config", s.requireAuth(http.HandlerFunc(s.handlePutConfig)))
	mux.Handle("GET /api/services", s.requireAuth(http.HandlerFunc(s.handleListServices)))
	mux.Handle("GET /api/maintenance", s.requireAuth(http.HandlerFunc(s.handleGetMaintenance)))
	mux.Handle("PUT /api/maintenance", s.requireAuth(http.HandlerFunc(s.handlePutMaintenance)))
	mux.Handle("POST /api/maintenance/run", s.requireAuth(http.HandlerFunc(s.handleRunMaintenance)))
	mux.Handle("GET /api/backups", s.requireAuth(http.HandlerFunc(s.handleGetBackups)))
	mux.Handle("PUT /api/backups/settings", s.requireAuth(http.HandlerFunc(s.handlePutBackupSettings)))
	mux.Handle("POST /api/backups/run", s.requireAuth(http.HandlerFunc(s.handleRunBackup)))
	mux.Handle("GET /api/backups/{name}", s.requireAuth(http.HandlerFunc(s.handleDownloadBackup)))
	mux.Handle("DELETE /api/backups/{name}", s.requireAuth(http.HandlerFunc(s.handleDeleteBackup)))
	mux.Handle("POST /api/services/{name}/restart", s.requireAuth(http.HandlerFunc(s.handleRestartService)))
	mux.Handle("GET /api/doors", s.requireAuth(http.HandlerFunc(s.handleListDoors)))
	mux.Handle("PUT /api/doors", s.requireAuth(http.HandlerFunc(s.handlePutDoors)))
	mux.Handle("GET /api/doors/templates", s.requireAuth(http.HandlerFunc(s.handleListDoorTemplates)))
	mux.Handle("POST /api/doors/templates/{id}", s.requireAuth(http.HandlerFunc(s.handleAddDoorFromTemplate)))
	mux.Handle("GET /api/doors/mrc", s.requireAuth(http.HandlerFunc(s.handleGetMRCConfig)))
	mux.Handle("PUT /api/doors/mrc", s.requireAuth(http.HandlerFunc(s.handlePutMRCConfig)))
	mux.Handle("POST /api/binkp/test-connection", s.requireAuth(http.HandlerFunc(s.handleTestBinkpConnection)))
	mux.Handle("POST /api/binkp/send-now", s.requireAuth(http.HandlerFunc(s.handleSendNowBinkp)))
	mux.Handle("POST /api/binkp/areafix/changes", s.requireAuth(http.HandlerFunc(s.handleRequestAreafixChanges)))
	mux.Handle("POST /api/binkp/areafix/list", s.requireAuth(http.HandlerFunc(s.handleRequestAreafixList)))
	mux.Handle("GET /api/binkp/areafix/list-reply", s.requireAuth(http.HandlerFunc(s.handleGetAreafixListReply)))
	mux.Handle("GET /api/binkp/areafix/subscriptions", s.requireAuth(http.HandlerFunc(s.handleListAreafixSubscriptions)))
	mux.Handle("GET /api/binkp/areafix/grants", s.requireAuth(http.HandlerFunc(s.handleListAreafixGrants)))
	mux.Handle("PUT /api/binkp/areafix/grants", s.requireAuth(http.HandlerFunc(s.handleSetAreafixGrants)))
	mux.Handle("GET /api/netmail/unresolved", s.requireAuth(http.HandlerFunc(s.handleListUnresolvedNetmail)))
	mux.Handle("GET /api/netmail/unresolved/{id}", s.requireAuth(http.HandlerFunc(s.handleGetUnresolvedNetmail)))
	mux.Handle("DELETE /api/netmail/unresolved/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteUnresolvedNetmail)))
	mux.Handle("POST /api/netmail/unresolved/batch-delete", s.requireAuth(http.HandlerFunc(s.handleBatchDeleteUnresolvedNetmail)))
	mux.Handle("GET /api/archive", s.requireAuth(http.HandlerFunc(s.handleListArchive)))
	mux.Handle("GET /api/archive/{id}/download", s.requireAuth(http.HandlerFunc(s.handleDownloadArchiveEntry)))
	mux.Handle("GET /api/archive/{id}/inspect", s.requireAuth(http.HandlerFunc(s.handleInspectArchiveEntry)))
	mux.Handle("DELETE /api/archive/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteArchiveEntry)))
	mux.Handle("POST /api/archive/retoss", s.requireAuth(http.HandlerFunc(s.handleRetossArchiveEntries)))
	mux.Handle("GET /api/dashboard", s.requireAuth(http.HandlerFunc(s.handleDashboard)))
	mux.Handle("GET /api/users", s.requireAuth(http.HandlerFunc(s.handleListUsers)))
	mux.Handle("PUT /api/users/{id}", s.requireAuth(http.HandlerFunc(s.handleSetUserSecurityLevel)))
	mux.Handle("PUT /api/users/{id}/password", s.requireAuth(http.HandlerFunc(s.handleSetUserPassword)))
	mux.Handle("DELETE /api/users/{id}/totp", s.requireAuth(http.HandlerFunc(s.handleResetUserTOTP)))
	mux.Handle("GET /api/account/totp", s.requireAuth(http.HandlerFunc(s.handleGetTOTP)))
	mux.Handle("POST /api/account/totp/start", s.requireAuth(http.HandlerFunc(s.handleStartTOTP)))
	mux.Handle("POST /api/account/totp/confirm", s.requireAuth(http.HandlerFunc(s.handleConfirmTOTP)))
	mux.Handle("POST /api/account/totp/disable", s.requireAuth(http.HandlerFunc(s.handleDisableTOTP)))
	mux.Handle("POST /api/account/totp/recovery", s.requireAuth(http.HandlerFunc(s.handleNewRecoveryCodes)))
	mux.Handle("POST /api/users/{id}/approve", s.requireAuth(http.HandlerFunc(s.handleApproveUser)))
	mux.Handle("DELETE /api/users/{id}", s.requireAuth(http.HandlerFunc(s.handleDeletePendingUser)))
	mux.Handle("GET /api/chat/rooms", s.requireAuth(http.HandlerFunc(s.handleListChatRooms)))
	mux.Handle("GET /api/chat/rooms/{room}", s.requireAuth(http.HandlerFunc(s.handleGetChatRoom)))
	mux.Handle("POST /api/chat/rooms/{room}", s.requireAuth(http.HandlerFunc(s.handleChatAction)))
	mux.Handle("GET /api/oneliners", s.requireAuth(http.HandlerFunc(s.handleListOneliners)))
	mux.Handle("DELETE /api/oneliners/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteOneliner)))
	mux.Handle("GET /api/nodelists", s.requireAuth(http.HandlerFunc(s.handleNodelistStatus)))
	mux.Handle("POST /api/nodelists/sync", s.requireAuth(http.HandlerFunc(s.handleNodelistSync)))
	mux.Handle("GET /api/polls", s.requireAuth(http.HandlerFunc(s.handleListPolls)))
	mux.Handle("POST /api/polls", s.requireAuth(http.HandlerFunc(s.handleCreatePoll)))
	mux.Handle("POST /api/polls/{id}/close", s.requireAuth(http.HandlerFunc(s.handleClosePoll)))
	mux.Handle("DELETE /api/polls/{id}", s.requireAuth(http.HandlerFunc(s.handleDeletePoll)))
	mux.Handle("GET /api/bbslist", s.requireAuth(http.HandlerFunc(s.handleListBBSList)))
	mux.Handle("DELETE /api/bbslist/{id}", s.requireAuth(http.HandlerFunc(s.handleAdminDeleteBBSListEntry)))
	mux.Handle("GET /api/security", s.requireAuth(http.HandlerFunc(s.handleGetSecurity)))
	mux.Handle("PUT /api/security/settings", s.requireAuth(http.HandlerFunc(s.handlePutSecuritySettings)))
	mux.Handle("POST /api/security/unlock", s.requireAuth(http.HandlerFunc(s.handleUnlockIP)))
	mux.Handle("POST /api/security/rules", s.requireAuth(http.HandlerFunc(s.handleAddIPRule)))
	mux.Handle("DELETE /api/security/rules", s.requireAuth(http.HandlerFunc(s.handleDeleteIPRule)))

	mux.Handle("GET /api/message-areas", s.requireAuth(http.HandlerFunc(s.handleListMessageAreas)))
	mux.Handle("POST /api/message-areas", s.requireAuth(http.HandlerFunc(s.handleCreateMessageArea)))
	mux.Handle("PUT /api/message-areas/{id}", s.requireAuth(http.HandlerFunc(s.handleUpdateMessageArea)))
	mux.Handle("DELETE /api/message-areas/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteMessageArea)))

	mux.Handle("GET /api/file-areas", s.requireAuth(http.HandlerFunc(s.handleListFileAreas)))
	mux.Handle("POST /api/file-areas", s.requireAuth(http.HandlerFunc(s.handleCreateFileArea)))
	mux.Handle("PUT /api/file-areas/{id}", s.requireAuth(http.HandlerFunc(s.handleUpdateFileArea)))
	mux.Handle("DELETE /api/file-areas/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteFileArea)))
	mux.Handle("GET /api/file-areas/{id}/files", s.requireAuth(http.HandlerFunc(s.handleListAreaFiles)))
	mux.Handle("POST /api/file-areas/{id}/files", s.requireAuth(http.HandlerFunc(s.handleUploadAreaFile)))
	mux.Handle("DELETE /api/files/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteFile)))

	mux.Handle("GET /api/groups", s.requireAuth(http.HandlerFunc(s.handleListGroups)))
	mux.Handle("GET /api/pending-areas", s.requireAuth(http.HandlerFunc(s.handleListPendingAreas)))
	mux.Handle("POST /api/pending-areas/message-areas/{id}/approve", s.requireAuth(http.HandlerFunc(s.handleApprovePendingMessageArea)))
	mux.Handle("POST /api/pending-areas/file-areas/{id}/approve", s.requireAuth(http.HandlerFunc(s.handleApprovePendingFileArea)))

	mux.Handle("GET /api/screens", s.requireAuth(http.HandlerFunc(s.handleListScreens)))
	mux.Handle("POST /api/screens", s.requireAuth(http.HandlerFunc(s.handleCreateScreen)))
	mux.Handle("POST /api/screens/import", s.requireAuth(http.HandlerFunc(s.handleImportScreen)))
	mux.Handle("GET /api/screens/{name}", s.requireAuth(http.HandlerFunc(s.handlePreviewScreen)))
	mux.Handle("DELETE /api/screens/{name}", s.requireAuth(http.HandlerFunc(s.handleDeleteScreen)))
	mux.Handle("GET /api/screens/{name}/grid", s.requireAuth(http.HandlerFunc(s.handleGetScreenGrid)))
	mux.Handle("PUT /api/screens/{name}/grid", s.requireAuth(http.HandlerFunc(s.handleSaveScreenGrid)))

	mux.Handle("GET /api/menus", s.requireAuth(http.HandlerFunc(s.handleListMenus)))
	mux.Handle("PUT /api/menus/{name}/items/{key}", s.requireAuth(http.HandlerFunc(s.handleSetMenuItemSL)))

	mux.Handle("GET /api/logs", s.requireAuth(http.HandlerFunc(s.handleListLogs)))
	mux.Handle("GET /api/binkp/sessions", s.requireAuth(http.HandlerFunc(s.handleListBinkpSessions)))
	mux.Handle("GET /api/binkp/sessions/{id}/transcript", s.requireAuth(http.HandlerFunc(s.handleGetBinkpSessionTranscript)))

	// BBS user portal: open to any registered account, gated per
	// endpoint/area (see requireBBSUser's own doc comment), not just
	// sysop-level ones.
	mux.HandleFunc("POST /api/bbs/auth/login", s.handleBBSLogin)
	mux.Handle("GET /api/bbs/message-areas", s.requireBBSUser(http.HandlerFunc(s.handleListBBSMessageAreas)))
	mux.Handle("GET /api/bbs/message-areas/{id}/messages", s.requireBBSUser(http.HandlerFunc(s.handleListBBSMessages)))
	mux.Handle("POST /api/bbs/message-areas/{id}/messages", s.requireBBSUser(http.HandlerFunc(s.handlePostBBSMessage)))
	mux.Handle("GET /api/bbs/message-areas/{id}/first-unread", s.requireBBSUser(http.HandlerFunc(s.handleFirstUnreadMessagePosition)))
	mux.Handle("POST /api/bbs/message-areas/{id}/mark-read", s.requireBBSUser(http.HandlerFunc(s.handleMarkBBSAreaRead)))
	mux.Handle("GET /api/bbs/messages/{id}", s.requireBBSUser(http.HandlerFunc(s.handleGetBBSMessage)))
	mux.Handle("GET /api/bbs/push/key", s.requireBBSUser(http.HandlerFunc(s.handleGetPushKey)))
	mux.Handle("GET /api/bbs/push/subscription", s.requireBBSUser(http.HandlerFunc(s.handleGetPushSubscription)))
	mux.Handle("PUT /api/bbs/push/subscription", s.requireBBSUser(http.HandlerFunc(s.handlePutPushSubscription)))
	mux.Handle("DELETE /api/bbs/push/subscription", s.requireBBSUser(http.HandlerFunc(s.handleDeletePushSubscription)))
	mux.Handle("POST /api/bbs/push/test", s.requireBBSUser(http.HandlerFunc(s.handleTestPush)))
	mux.Handle("GET /api/bbs/nodelist", s.requireBBSUser(http.HandlerFunc(s.handleSearchNodelist)))
	mux.Handle("GET /api/bbs/nodelist/lookup", s.requireBBSUser(http.HandlerFunc(s.handleLookupNodelist)))
	mux.Handle("GET /api/bbs/polls", s.requireBBSUser(http.HandlerFunc(s.handleListBBSPolls)))
	mux.Handle("POST /api/bbs/polls/{id}/vote", s.requireBBSUser(http.HandlerFunc(s.handleVoteBBSPoll)))
	mux.Handle("GET /api/bbs/bbslist", s.requireBBSUser(http.HandlerFunc(s.handleListBBSList)))
	mux.Handle("POST /api/bbs/bbslist", s.requireBBSUser(http.HandlerFunc(s.handleSaveBBSListEntry)))
	mux.Handle("PUT /api/bbs/bbslist/{id}", s.requireBBSUser(http.HandlerFunc(s.handleSaveBBSListEntry)))
	mux.Handle("DELETE /api/bbs/bbslist/{id}", s.requireBBSUser(http.HandlerFunc(s.handleDeleteBBSListEntry)))
	mux.Handle("GET /api/bbs/netmail", s.requireBBSUser(http.HandlerFunc(s.handleListBBSNetmail)))
	mux.Handle("GET /api/bbs/last-callers", s.requireBBSUser(http.HandlerFunc(s.handleListLastCallers)))
	mux.Handle("GET /api/bbs/netmail/sent", s.requireBBSUser(http.HandlerFunc(s.handleListBBSNetmailSent)))
	mux.Handle("POST /api/bbs/netmail", s.requireBBSUser(http.HandlerFunc(s.handleSendBBSNetmail)))
	mux.Handle("GET /api/bbs/netmail/{id}", s.requireBBSUser(http.HandlerFunc(s.handleGetBBSNetmail)))
	mux.Handle("DELETE /api/bbs/netmail/{id}", s.requireBBSUser(http.HandlerFunc(s.handleDeleteBBSNetmail)))
	mux.Handle("GET /api/bbs/file-areas", s.requireBBSUser(http.HandlerFunc(s.handleListBBSFileAreas)))
	mux.Handle("GET /api/bbs/file-areas/{id}/files", s.requireBBSUser(http.HandlerFunc(s.handleListBBSAreaFiles)))
	mux.Handle("POST /api/bbs/file-areas/{id}/files", s.requireBBSUser(http.HandlerFunc(s.handleUploadBBSAreaFile)))
	mux.Handle("GET /api/bbs/files/{id}", s.requireBBSUser(http.HandlerFunc(s.handleGetBBSFile)))
	mux.Handle("GET /api/bbs/files/{id}/download", s.requireBBSUser(http.HandlerFunc(s.handleDownloadBBSFile)))
	mux.Handle("GET /api/bbs/files/{id}/preview", s.requireBBSUser(http.HandlerFunc(s.handlePreviewBBSFile)))
	mux.Handle("GET /api/bbs/files/{id}/preview-raw", s.requireBBSUser(http.HandlerFunc(s.handlePreviewRawBBSFile)))
	mux.Handle("GET /api/bbs/files/{id}/preview-entry", s.requireBBSUser(http.HandlerFunc(s.handlePreviewBBSFileEntry)))
	mux.Handle("GET /api/bbs/files/{id}/preview-entry-raw", s.requireBBSUser(http.HandlerFunc(s.handlePreviewRawBBSFileEntry)))
	mux.Handle("GET /api/bbs/profile", s.requireBBSUser(http.HandlerFunc(s.handleGetBBSProfile)))
	mux.Handle("PUT /api/bbs/profile", s.requireBBSUser(http.HandlerFunc(s.handleUpdateBBSProfile)))
	mux.Handle("POST /api/bbs/profile/password", s.requireBBSUser(http.HandlerFunc(s.handleChangeBBSPassword)))
	mux.Handle("GET /api/bbs/qwk/areas", s.requireBBSUser(http.HandlerFunc(s.handleListBBSQWKAreas)))
	mux.Handle("PUT /api/bbs/qwk/areas", s.requireBBSUser(http.HandlerFunc(s.handleSetBBSQWKAreas)))
	mux.Handle("GET /api/bbs/qwk/download", s.requireBBSUser(http.HandlerFunc(s.handleDownloadBBSQWK)))
	mux.Handle("POST /api/bbs/qwk/upload", s.requireBBSUser(http.HandlerFunc(s.handleUploadBBSQWKReply)))

	if s.StaticDir != "" {
		if _, err := os.Stat(s.StaticDir); err == nil {
			mux.Handle("/", spaFileServer(s.StaticDir))
		}
	}

	return withCORS(mux)
}

// spaFileServer serves files from dir, falling back to index.html for
// any path that doesn't match a real file. The SvelteKit build here
// runs as a client-side-only SPA (see web/vite.config.ts), so
// client-side routes like /login and /settings only exist as
// in-browser router state, not as files on disk.
func spaFileServer(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fullPath := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

// withCORS allows the SvelteKit dev server (a different origin during
// development) to call the API. The admin API is protected by JWT
// bearer auth rather than cookies, so a permissive origin reflection
// carries no CSRF risk.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
