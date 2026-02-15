<script lang="ts">
  import type { Config, Server } from '../types'

  interface Props {
    config: Config
    onUpdate: (updates: Partial<Config>) => void
  }

  let { config, onUpdate }: Props = $props()

  let newServerName = $state('')
  let newServerPort = $state('')
  let newServerCategory = $state('')

  function handleAdd() {
    if (!newServerName.trim() || !newServerPort || !newServerCategory.trim()) {
      return
    }

    const port = parseInt(newServerPort)
    if (port < 1 || port > 65535) {
      alert('Port must be between 1 and 65535')
      return
    }

    const newServer: Server = {
      name: newServerName.trim(),
      ip: config.server_ip,
      port,
      category: newServerCategory.trim(),
    }

    onUpdate({
      servers: [...config.servers, newServer],
    })

    newServerName = ''
    newServerPort = ''
    newServerCategory = ''
  }

  function handleRemove(index: number) {
    const servers = [...config.servers]
    servers.splice(index, 1)
    onUpdate({ servers })
  }

  function handleUpdatePort(index: number, port: string) {
    const portNum = parseInt(port)
    if (portNum >= 1 && portNum <= 65535) {
      const servers = [...config.servers]
      servers[index] = { ...servers[index], port: portNum }
      onUpdate({ servers })
    }
  }

  function handleUpdateCategory(index: number, category: string) {
    const servers = [...config.servers]
    servers[index] = { ...servers[index], category }
    onUpdate({ servers })
  }
</script>

<div class="panel">
  <h3>Servers</h3>

  <div class="server-list">
    {#each config.servers as server, index (index)}
      <div class="server-item">
        <span class="server-name">{server.name}</span>
        <input
          type="number"
          value={server.port}
          oninput={(e) => handleUpdatePort(index, e.currentTarget.value)}
          min="1"
          max="65535"
          class="port-input"
        />
        <select
          value={server.category}
          onchange={(e) => handleUpdateCategory(index, e.currentTarget.value)}
          class="category-select"
        >
          {#each config.category_order as cat}
            <option value={cat}>{cat}</option>
          {/each}
        </select>
        <button
          onclick={() => handleRemove(index)}
          class="btn-remove"
          aria-label={`Remove ${server.name}`}
        >
          ×
        </button>
      </div>
    {/each}
  </div>

  <div class="add-form">
    <input
      type="text"
      bind:value={newServerName}
      placeholder="Server name"
    />
    <input
      type="number"
      bind:value={newServerPort}
      placeholder="Port (1-65535)"
      min="1"
      max="65535"
    />
    <select bind:value={newServerCategory} class="category-select">
      <option value="">Select category</option>
      {#each config.category_order as cat}
        <option value={cat}>{cat}</option>
      {/each}
    </select>
    <button onclick={handleAdd} class="btn-add">Add</button>
  </div>
</div>

<style>
  .panel {
    background: #252535;
    padding: 1.5rem;
    border-radius: 8px;
    margin-bottom: 1.5rem;
  }

  h3 {
    color: #ffffff;
    margin-bottom: 1rem;
    font-size: 1.125rem;
  }

  .server-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin-bottom: 1rem;
  }

  .server-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem;
    background: #1a1a25;
    border-radius: 4px;
  }

  .server-name {
    flex: 1;
    color: #e0e0e0;
  }

  .port-input {
    width: 80px;
    padding: 0.375rem;
    background: #252535;
    border: 1px solid #353545;
    border-radius: 4px;
    color: #e0e0e0;
  }

  .category-select {
    padding: 0.375rem;
    background: #252535;
    border: 1px solid #353545;
    border-radius: 4px;
    color: #e0e0e0;
  }

  .btn-remove {
    width: 28px;
    height: 28px;
    padding: 0;
    background: #ff4a4a;
    color: white;
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-size: 1.25rem;
    line-height: 1;
  }

  .btn-remove:hover {
    background: #ff3a3a;
  }

  .add-form {
    display: flex;
    gap: 0.5rem;
  }

  .add-form input,
  .add-form select {
    padding: 0.5rem;
    background: #1a1a25;
    border: 1px solid #353545;
    border-radius: 4px;
    color: #e0e0e0;
  }

  .add-form input {
    flex: 1;
  }

  .btn-add {
    padding: 0.5rem 1rem;
    background: #4a9eff;
    color: white;
    border: none;
    border-radius: 4px;
    cursor: pointer;
  }

  .btn-add:hover {
    background: #3a8eef;
  }
</style>
