# Architecture Visualization: Current vs. Recommended

## Current Architecture (Problematic)

```
┌─────────────────────────────────────────────────────────────────────┐
│                        main.go (God Object)                        │
│                                                                   │
│  ┌─────────────────────┐  ┌─────────────────┐  ┌─────────────────┐ │
│  │    Bot Struct        │  │ ConfigManager   │  │    Static Files │ │
│  │                     │  │                 │  │ (webfront/dist) │ │
│  │  • Discord Session  │  │  • Config loading│  │                 │ │
│  │  • API Server       │  │  • File watching│  │ • SPA routing   │ │
│  │  • Web Server       │  │  • Validation  │  │ • HTTP.FileServer│ │
│  │  • Message Mutex    │  │  • Backup logic │  │                 │ │
│  │  • Config Access    │  │  • Atomic writes│  │                 │ │
│  │                     │  │                 │  │                 │ │
│  └─────────────────────┘  └─────────────────┘  └─────────────────┘ │
│          │                     │                     │            │
│          │                     │                     │            │
│          v                     v                     v            │
│ ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐      │
│ │ Discord API    │  │ Config File     │  │ HTTP Requests   │      │
│ │ (external)     │  │ (filesystem)   │  │ (external)      │      │
│ └─────────────────┘  └─────────────────┘  └─────────────────┘      │
└─────────────────────────────────────────────────────────────────────┘
```

**Problems with Current Architecture:**
1. **Tight Coupling**: All components depend directly on the main binary
2. **Single Responsibility Violation**: Bot struct handles Discord, API, Web, and Config
3. **Cross-Cutting Changes**: Adding a feature requires changes to multiple parts of main.go
4. **Difficult Testing**: Components can't be tested independently
5. **Scalability Issues**: Can't scale individual components independently
6. **No Fault Isolation**: Failure in one component affects others

## Recommended Architecture (Microservices)

```
┌─────────────────────────────────────────────────────────────────────┐
│                  Event Bus / Message Queue                         │
│                        (RabbitMQ/Redis)                          │
└─────────────────────────────────────────────────────────────────────┘
              │            │            │            │
┌─────────────┼────────────┼────────────┼────────────┐
│             │            │            │            │
▼            ▼            ▼            ▼            ▼

┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│   Discord Bot   │  │  API Server     │  │ Web Frontend    │  │ Config Service  │
│   Service       │  │   Service       │  │   Service       │  │   Service       │
│                 │  │                 │  │                 │  │                 │
│ • Discord API   │  │ • REST Endpoints │  │ • Static Files  │  │ • Config DB     │
│ • Message Logic │  │ • Auth Layer    │  │ • SPA Router    │  │ • File Watcher  │
│ • Event Handler │  │ • Rate Limit    │  │ • Cache Headers │  │ • Validation    │
│ • Health Check  │  │ • CORS          │  │ • CDN Support   │  │ • Backup/Restore│
│                 │  │ • Metrics      │  │ • PWA Support   │  │ • Config Events │
│                 │  │ • Metrics      │  │ • Analytics     │  │ • Config Events │
└─────────────────┘  └─────────────────┘  └─────────────────┘  └─────────────────┘
      │                    │                    │                    │
      │                    │                    │                    │
      ▼                    ▼                    ▼                    ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│ Discord         │  │ Database        │  │ Static File     │  │ File System     │
│ (external)      │  │ (external)      │  │ Storage         │  │ (external)      │
└─────────────────┘  └─────────────────┘  └─────────────────┘  └─────────────────┘
```

**Benefits of Recommended Architecture:**
1. **Loose Coupling**: Components communicate via well-defined APIs
2. **Single Responsibility**: Each service has one clear purpose
3. **Independent Deployability**: Services can be deployed separately
4. **Fault Isolation**: Failure in one service doesn't crash others
5. **Scalability**: Each service can scale based on its needs
6. **Testability**: Services can be tested in isolation

## Detailed Component Responsibilities

### 1. Discord Bot Service
```go
// Focus: Pure Discord functionality
type DiscordService struct {
    session    *discordgo.Session
    eventBus   event.EventBus
    config     discord.Config
    cache      *DiscordCache
}

// Responsibilities:
- Discord API interactions
- Message formatting and updates
- Event handling
- Authentication
- Health checks
```

### 2. API Server Service
```go
// Focus: HTTP API and external integrations
type APIService struct {
    server     *http.Server
    middleware []middleware.Middleware
    config     api.Config
    rateLimiter *rate.RateLimiter
    auth       auth.Authenticator
}

// Responsibilities:
- HTTP endpoint handling
- Request/response processing
- Authentication
- Rate limiting
- CORS management
- Monitoring/metrics
```

### 3. Web Frontend Service
```go
// Focus: Static content delivery
type WebService struct {
    server     *http.Server
    fileServer http.FileSystem
    cache      *ResponseCache
    config     web.Config
}

// Responsibilities:
- Static file serving
- SPA routing
- Caching
- Compression
- CDN integration
```

### 4. Config Service
```go
// Focus: Configuration management
type ConfigService struct {
    repository  ConfigRepository
    validator   ConfigValidator
    watchdog    FileWatcher
    eventBus   event.EventBus
}

// Responsibilities:
- Configuration loading/saving
- Validation
- File watching
- Backup/restore
- Event notifications
```

## Event Flow Example

1. **Config Change Event:**
   ```
   Config Service → Event Bus → Discord Bot (updates status)
                           → API Server (config refresh)
                           → Web Service (config refresh)
   ```

2. **Server Status Update:**
   ```
   Server Monitoring → Event Bus → Discord Bot (status message)
                          → Config Service (logging)
                          → Monitoring Dashboard
   ```

3. **Health Check Events:**
   ```
   Health Check Service → Event Bus → Monitoring Dashboard
                                → Alerting System
   ```

## Transition Strategy

1. **Phase 1**: Extract ConfigManager into separate service
2. **Phase 2**: Move API server to separate process
3. **Phase 3**: Extract web serving functionality
4. **Phase 4**: Move Discord bot to separate service
5. **Phase 5**: Implement event bus for communication
6. **Phase 6**: Add service discovery and monitoring

## Migration Benefits

1. **Reduced Risk**: Changes are isolated to specific services
2. **Better Testing**: Services can be tested independently
3. **Easier Deployment**: Gradual migration with feature flags
4. **Improved Monitoring**: Service-level metrics and observability
5. **Scalability**: Each service scales independently
6. **Maintainability**: Clear boundaries and responsibilities