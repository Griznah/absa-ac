// Config matches Go Config struct from main.go
export interface Config {
  server_ip: string
  update_interval: number
  category_order: string[]
  category_emojis: Record<string, string>
  servers: Server[]
}

export interface Server {
  name: string
  ip: string
  port: number
  category: string
}

export interface CategoryEmoji {
  category: string
  emoji: string
}

export interface ErrorResponse {
  error: string
  message?: string
}
