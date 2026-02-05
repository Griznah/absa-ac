package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ================= CONFIG =================

var (
	//	discordToken   string
	//	channelID      string
	serverIP       string
	updateInterval = 30 // seconds
)

type Server struct {
	Name     string
	IP       string
	Port     int
	Category string
}

var servers = []Server{
	// -------- DRIFT --------
	{
		Name:     "ABSA#1 Drift | Rotating Maps | BDC 4.0",
		IP:       "", // Will be set from serverIP env var
		Port:     8081,
		Category: "Drift",
	},
	{
		Name:     "ABSA#2 Drift | Rotating Maps | Gravy Garage",
		IP:       "",
		Port:     8082,
		Category: "Drift",
	},
	{
		Name:     "ABSA#3 Drift | Rotating Maps | SWARM 3.2",
		IP:       "",
		Port:     8083,
		Category: "Drift",
	},
	{
		Name:     "ABSA#4 Drift | Rotating Maps | SWARM 3.2",
		IP:       "",
		Port:     8084,
		Category: "Drift",
	},
	{
		Name:     "ABSA#8 Drift | Rotating Maps | SWARM 3.2 Touge",
		IP:       "",
		Port:     8088,
		Category: "Drift",
	},
	// -------- TOUGE --------
	{
		Name:     "ABSA#6 Race | Touge FAST Lap",
		IP:       "",
		Port:     8086,
		Category: "Touge",
	},
	// -------- TRACK --------
	{
		Name:     "ABSA#5 Race | Nordschleife Tourist FAST Lap",
		IP:       "",
		Port:     8085,
		Category: "Track",
	},
	{
		Name:     "ABSA#7 Let's play tag!  | Rotating Maps",
		IP:       "",
		Port:     8087,
		Category: "Track",
	},
	{
		Name:     "ABSA#9 SRP | SRP Traffic",
		IP:       "",
		Port:     8089,
		Category: "Track",
	},
	{
		Name:     "ABSA#10 SRP | SRP Traffic",
		IP:       "",
		Port:     8090,
		Category: "Track",
	},
}

var categoryOrder = []string{"Drift", "Touge", "Track"}

var categoryEmojis = map[string]string{
	"Drift": "\U0001F7E3", // 🟣
	"Touge": "\U0001F7E0", // 🟠
	"Track": "\U0001F535", // 🔵
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
	serverMessage *discordgo.Message
	messageMutex  sync.RWMutex
}

// ================= HTTP CLIENT =================

var httpClient = &http.Client{
	Timeout: 2 * time.Second,
}

func fetchAllServers() []ServerInfo {
	var wg sync.WaitGroup
	infos := make([]ServerInfo, len(servers))
	mu := sync.Mutex{}

	for i, server := range servers {
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

func buildEmbed(infos []ServerInfo) *discordgo.MessageEmbed {
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
			URL: fmt.Sprintf("http://%s/images/logo.png", serverIP),
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Updates every %d seconds", updateInterval),
		},
	}

	// Add fields by category
	for _, category := range categoryOrder {
		emoji := categoryEmojis[category]
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
	ticker := time.NewTicker(time.Duration(updateInterval) * time.Second)
	defer ticker.Stop()

	// Immediate first update
	b.performUpdate()

	for range ticker.C {
		b.performUpdate()
	}
}

func (b *Bot) performUpdate() {
	// Fetch all server info concurrently
	infos := fetchAllServers()

	// Build embed
	embed := buildEmbed(infos)

	// Update Discord message
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

func NewBot(token, channelID string) (*Bot, error) {
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

func validateConfig() (token, channelID, ip string, err error) {
	token = os.Getenv("DISCORD_TOKEN")
	if token == "" {
		return "", "", "", fmt.Errorf("DISCORD_TOKEN environment variable not set")
	}

	channelID = os.Getenv("CHANNEL_ID")
	if channelID == "" {
		return "", "", "", fmt.Errorf("CHANNEL_ID environment variable not set")
	}

	ip = os.Getenv("SERVER_IP")
	if ip == "" {
		return "", "", "", fmt.Errorf("SERVER_IP environment variable not set")
	}

	return token, channelID, ip, nil
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	token, channelID, ip, err := validateConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	//	discordToken = token
	//	channelID = channelID
	serverIP = ip

	// Update server IPs in the servers list
	for i := range servers {
		servers[i].IP = serverIP
	}

	bot, err := NewBot(token, channelID)
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
