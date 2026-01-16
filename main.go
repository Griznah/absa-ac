package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ================= ENV LOADING =================

// loadEnv reads a .env file and sets environment variables
// Only sets variables that aren't already set in the environment
func loadEnv() error {
	envPath := ".env"

	file, err := os.Open(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			// .env file is optional, not an error
			return nil
		}
		return fmt.Errorf("failed to open .env file: %w", err)
	}
	defer file.Close()

	log.Printf("Loading environment variables from: %s", envPath)

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			log.Printf("Warning: invalid line %d in .env, skipping: %s", lineNum, line)
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			value = strings.Trim(value, "\"")
		} else if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
			value = strings.Trim(value, "'")
		}

		// Only set if not already in environment
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				log.Printf("Warning: failed to set %s: %v", key, err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading .env file: %w", err)
	}

	return nil
}

// ================= CONFIG =================

var (
	discordToken string
	channelID    string
)

type Server struct {
	Name     string
	IP       string
	Port     int
	Category string
}

// ================= TYPES =================

type ServerInfo struct {
	Name       string
	Category   string
	Map        string
	Players    string // "X/Y" format
	NumPlayers int    // For sorting/totaling (-1 = offline)
	IP         string
	Port       int
}

type Bot struct {
	session       *discordgo.Session
	channelID     string
	config        *Config
	serverMessage *discordgo.Message
	messageMutex  sync.RWMutex
}

// Config holds application configuration loaded from config.json
type Config struct {
	ServerIP        string            `json:"server_ip"`
	UpdateInterval  int               `json:"update_interval"`
	CategoryOrder   []string          `json:"category_order"`
	CategoryEmojis  map[string]string `json:"category_emojis"`
	Servers         []Server          `json:"servers"`
}

// loadConfig reads and parses config.json with fallback logic
func loadConfig(providedPath string) (*Config, error) {
	// If explicitly provided, only try that path
	if providedPath != "" {
		log.Printf("Loading config from provided path: %s", providedPath)
		data, err := os.ReadFile(providedPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config from %s: %w", providedPath, err)
		}

		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config from %s: %w", providedPath, err)
		}

		log.Printf("Successfully loaded config from: %s", providedPath)
		return &cfg, nil
	}

	// Otherwise, try default locations in priority order
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	defaultPaths := []string{
		"/data/config.json",
		filepath.Join(wd, "config.json"),
	}

	var errors []string
	for _, path := range defaultPaths {
		log.Printf("Attempting to load config from: %s", path)

		data, err := os.ReadFile(path)
		if err != nil {
			errors = append(errors, fmt.Sprintf("  %s: %v", path, err))
			continue
		}

		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config from %s: %w", path, err)
		}

		log.Printf("Successfully loaded config from: %s", path)
		return &cfg, nil
	}

	return nil, fmt.Errorf("failed to load config from any default location:\n%s", strings.Join(errors, "\n"))
}

// validateConfigStruct performs fail-fast validation on loaded config
func validateConfigStruct(cfg *Config) {
	// Validate ServerIP
	if cfg.ServerIP == "" {
		log.Fatalf("Configuration error: server_ip cannot be empty")
	}

	// Validate UpdateInterval (minimum 1 second)
	if cfg.UpdateInterval < 1 {
		log.Fatalf("Configuration error: update_interval must be at least 1 second (got: %d)", cfg.UpdateInterval)
	}

	// Validate CategoryOrder
	if len(cfg.CategoryOrder) == 0 {
		log.Fatalf("Configuration error: category_order cannot be empty")
	}

	// Build category lookup map for O(1) validation
	categoryMap := make(map[string]bool)
	for _, cat := range cfg.CategoryOrder {
		categoryMap[cat] = true
	}

	// Validate all categories have emojis
	for _, cat := range cfg.CategoryOrder {
		if _, exists := cfg.CategoryEmojis[cat]; !exists {
			log.Fatalf("Configuration error: category '%s' is in category_order but missing from category_emojis", cat)
		}
	}

	// Validate servers
	for i, server := range cfg.Servers {
		if server.Name == "" {
			log.Fatalf("Configuration error: server at index %d has empty name", i)
		}

		if server.Port < 1 || server.Port > 65535 {
			log.Fatalf("Configuration error: server '%s' has invalid port: %d (valid range: 1-65535)", server.Name, server.Port)
		}

		if server.Category == "" {
			log.Fatalf("Configuration error: server '%s' has empty category", server.Name)
		}

		// Validate server category exists in CategoryOrder
		if !categoryMap[server.Category] {
			log.Fatalf("Configuration error: server '%s' has category '%s' which is not defined in category_order", server.Name, server.Category)
		}
	}

	log.Printf("Configuration validated: %d servers across %d categories", len(cfg.Servers), len(cfg.CategoryOrder))
}

// ================= HTTP CLIENT =================

var httpClient = &http.Client{
	Timeout: 2 * time.Second,
}

func fetchAllServers(cfg *Config) []ServerInfo {
	var wg sync.WaitGroup
	infos := make([]ServerInfo, len(cfg.Servers))
	mu := sync.Mutex{}

	for i, server := range cfg.Servers {
		wg.Add(1)
		go func(idx int, s Server) {
			defer wg.Done()
			info := fetchServerInfo(s)

			mu.Lock()
			infos[idx] = info
			mu.Unlock()
		}(i, server)
	}

	wg.Wait()
	return infos
}

func fetchServerInfo(server Server) ServerInfo {
	url := fmt.Sprintf("http://%s:%d/info", server.IP, server.Port)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return offlineServerInfo(server)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return offlineServerInfo(server)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return offlineServerInfo(server)
	}

	var data struct {
		Clients    int    `json:"clients"`
		MaxClients int    `json:"maxclients"`
		Track      string `json:"track"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return offlineServerInfo(server)
	}

	trackName := filepath.Base(data.Track)
	if trackName == "." || trackName == "" {
		trackName = "Unknown"
	}

	return ServerInfo{
		Name:       server.Name,
		Category:   server.Category,
		Map:        trackName,
		Players:    fmt.Sprintf("%d/%d", data.Clients, data.MaxClients),
		NumPlayers: data.Clients,
		IP:         server.IP,
		Port:       server.Port,
	}
}

func offlineServerInfo(server Server) ServerInfo {
	return ServerInfo{
		Name:       server.Name,
		Category:   server.Category,
		Map:        "Offline",
		Players:    "0/0",
		NumPlayers: -1, // Negative indicates offline
		IP:         server.IP,
		Port:       server.Port,
	}
}

// ================= DISCORD INTEGRATION =================

func buildEmbed(infos []ServerInfo, cfg *Config) *discordgo.MessageEmbed {
	// Group servers and calculate totals
	grouped := make(map[string][]ServerInfo)
	categoryTotals := make(map[string]int)
	totalPlayers := 0

	for _, info := range infos {
		grouped[info.Category] = append(grouped[info.Category], info)
		if info.NumPlayers > 0 {
			categoryTotals[info.Category] += info.NumPlayers
			totalPlayers += info.NumPlayers
		}
	}

	// Build embed
	embed := &discordgo.MessageEmbed{
		Title:       "ABSA Official Servers",
		Description: fmt.Sprintf(":bust_in_silhouette: **Total Players:** %d", totalPlayers),
		Color:       0x00FF00, // Green
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://upload.wikimedia.org/wikipedia/commons/thumb/d/d9/Flag_of_Norway.svg/320px-Flag_of_Norway.svg.png",
		},
		Image: &discordgo.MessageEmbedImage{
			URL: fmt.Sprintf("http://%s/images/logo.png", cfg.ServerIP),
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Updates every %d seconds", cfg.UpdateInterval),
		},
	}

	// Append fields by category
	for _, category := range cfg.CategoryOrder {
		emoji := cfg.CategoryEmojis[category]
		total := categoryTotals[category]

		// Category header field
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%s **%s Servers — %d players**", emoji, category, total),
			Value:  "\u200b", // Zero-width space
			Inline: false,
		})

		// Individual server fields
		for _, info := range grouped[category] {
			statusEmoji := ":green_circle:"
			if info.NumPlayers < 0 {
				statusEmoji = ":red_circle:"
			}

			joinURL := fmt.Sprintf(
				"https://acstuff.club/s/q:race/online/join?ip=%s&httpPort=%d",
				info.IP, info.Port,
			)

			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name: fmt.Sprintf("%s %s", statusEmoji, info.Name),
				Value: fmt.Sprintf(
					"**Map:** %s\n**Players:** %s\n[Join Server](%s)",
					info.Map, info.Players, joinURL,
				),
				Inline: false,
			})
		}

		// Spacer after category
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "\u200b",
			Value:  "\u200b",
			Inline: false,
		})
	}

	return embed
}

func (b *Bot) getStatusMessage() *discordgo.Message {
	b.messageMutex.RLock()
	defer b.messageMutex.RUnlock()
	return b.serverMessage
}

func (b *Bot) setStatusMessage(msg *discordgo.Message) {
	b.messageMutex.Lock()
	defer b.messageMutex.Unlock()
	b.serverMessage = msg
}

func (b *Bot) updateStatusMessage(embed *discordgo.MessageEmbed) error {
	existing := b.getStatusMessage()

	var msg *discordgo.Message
	var err error

	if existing == nil {
		// Create new message
		msg, err = b.session.ChannelMessageSendEmbed(b.channelID, embed)
		if err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
		b.setStatusMessage(msg)
		log.Println("Initial status message posted")
	} else {
		// Edit existing message
		msg, err = b.session.ChannelMessageEditComplex(
			&discordgo.MessageEdit{
				ID:      existing.ID,
				Channel: b.channelID,
				Embed:   embed,
			},
		)
		if err != nil {
			// Message might have been deleted - recreate
			if restError, ok := err.(*discordgo.RESTError); ok && restError.Response != nil && restError.Response.StatusCode == 404 {
				msg, err = b.session.ChannelMessageSendEmbed(b.channelID, embed)
				if err != nil {
					return fmt.Errorf("failed to recreate message: %w", err)
				}
				b.setStatusMessage(msg)
				log.Println("Status message recreated (previous was deleted)")
				return nil
			}
			return fmt.Errorf("failed to edit message: %w", err)
		}
		b.setStatusMessage(msg)
		log.Println("Status message updated")
	}

	return nil
}

// ================= EVENT HANDLERS =================

func (b *Bot) onReady(s *discordgo.Session, event *discordgo.Ready) {
	log.Printf("✅ Logged in as %s", s.State.User.Username)

	// Clean up old messages
	if err := b.cleanupOldMessages(); err != nil {
		log.Printf("Warning: cleanup failed: %v", err)
	}

	// Start update loop in background goroutine
	go b.startUpdateLoop()
}

func (b *Bot) cleanupOldMessages() error {
	// Fetch messages (Discord API returns max 100 per request)
	messages, err := b.session.ChannelMessages(b.channelID, 100, "", "", "")
	if err != nil {
		return fmt.Errorf("failed to fetch messages: %w", err)
	}

	botUserID := b.session.State.User.ID
	deletedCount := 0

	for _, msg := range messages {
		if msg.Author.ID == botUserID {
			if err := b.session.ChannelMessageDelete(b.channelID, msg.ID); err != nil {
				log.Printf("Failed to delete message %s: %v", msg.ID, err)
			} else {
				deletedCount++
			}
		}
	}

	log.Printf("Cleaned up %d old bot messages", deletedCount)
	return nil
}

func (b *Bot) registerHandlers() {
	b.session.AddHandler(b.onReady)
}

// ================= UPDATE LOOP =================

func (b *Bot) startUpdateLoop() {
	ticker := time.NewTicker(time.Duration(b.config.UpdateInterval) * time.Second)
	defer ticker.Stop()

	// Immediate first update
	b.performUpdate()

	for range ticker.C {
		b.performUpdate()
	}
}

func (b *Bot) performUpdate() {
	// Fetch all server info concurrently
	infos := fetchAllServers(b.config)

	// Build embed
	embed := buildEmbed(infos, b.config)

	// Send updated embed to Discord
	if err := b.updateStatusMessage(embed); err != nil {
		log.Printf("Error updating status: %v", err)
	}
}

// ================= BOT CONSTRUCTION =================

func createDiscordSession(token string) (*discordgo.Session, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentGuildMessages

	return session, nil
}

func NewBot(cfg *Config, token, channelID string) (*Bot, error) {
	if token == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN environment variable not set")
	}
	if channelID == "" {
		return nil, fmt.Errorf("CHANNEL_ID environment variable not set")
	}

	session, err := createDiscordSession(token)
	if err != nil {
		return nil, err
	}

	return &Bot{
		session:   session,
		channelID: channelID,
		config:    cfg,
	}, nil
}

func (b *Bot) Start() error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open Discord connection: %w", err)
	}
	return nil
}

func (b *Bot) WaitForShutdown() {
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	<-sigchan
	log.Println("Shutting down...")

	if err := b.session.Close(); err != nil {
		log.Printf("Error closing Discord session: %v", err)
	}

	log.Println("Shutdown complete")
}

// ================= MAIN =================

func validateConfig() (token, channelID string, err error) {
	token = os.Getenv("DISCORD_TOKEN")
	if token == "" {
		return "", "", fmt.Errorf("DISCORD_TOKEN environment variable not set")
	}

	channelID = os.Getenv("CHANNEL_ID")
	if channelID == "" {
		return "", "", fmt.Errorf("CHANNEL_ID environment variable not set")
	}

	return token, channelID, nil
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Parse command-line flags for config path
	configPath := flag.String("c", "", "Path to config.json file")
	flag.StringVar(configPath, "config", "", "Path to config.json file")
	flag.Parse()

	// Load environment variables from .env file (optional)
	if err := loadEnv(); err != nil {
		log.Printf("Warning: %v", err)
	}

	token, channelID, err := validateConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	discordToken = token
	channelID = channelID

	// Load and validate config.json
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	validateConfigStruct(cfg)

	// Set server IPs from config
	for i := range cfg.Servers {
		cfg.Servers[i].IP = cfg.ServerIP
	}

	bot, err := NewBot(cfg, token, channelID)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	bot.registerHandlers()

	if err := bot.Start(); err != nil {
		log.Fatalf("Failed to start bot: %v", err)
	}

	// Wait for shutdown signal
	bot.WaitForShutdown()
}
