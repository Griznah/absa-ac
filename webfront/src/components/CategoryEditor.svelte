<script lang="ts">
  import type { Config } from '../types'

  interface Props {
    config: Config
    onUpdate: (updates: Partial<Config>) => void
  }

  let { config, onUpdate }: Props = $props()

  let newCategory = $state('')
  let newEmoji = $state('')

  function handleAdd() {
    if (!newCategory.trim() || !newEmoji.trim()) return

    const categoryOrder = [...config.category_order, newCategory.trim()]
    const categoryEmojis = {
      ...config.category_emojis,
      [newCategory.trim()]: newEmoji.trim(),
    }

    onUpdate({
      category_order: categoryOrder,
      category_emojis: categoryEmojis,
    })

    newCategory = ''
    newEmoji = ''
  }

  function handleRemove(category: string) {
    const categoryOrder = config.category_order.filter((c) => c !== category)
    const categoryEmojis = { ...config.category_emojis }
    delete categoryEmojis[category]

    onUpdate({
      category_order: categoryOrder,
      category_emojis: categoryEmojis,
    })
  }

  function handleEmojiChange(category: string, emoji: string) {
    onUpdate({
      category_emojis: {
        ...config.category_emojis,
        [category]: emoji,
      },
    })
  }
</script>

<div class="panel">
  <h3>Categories</h3>

  <div class="category-list">
    {#each config.category_order as category (category)}
      <div class="category-item">
        <span class="category-name">{category}</span>
        <input
          type="text"
          value={config.category_emojis[category] || ''}
          oninput={(e) => handleEmojiChange(category, e.currentTarget.value)}
          placeholder="emoji"
          class="emoji-input"
        />
        <button
          onclick={() => handleRemove(category)}
          class="btn-remove"
          aria-label={`Remove ${category}`}
        >
          ×
        </button>
      </div>
    {/each}
  </div>

  <div class="add-form">
    <input
      type="text"
      bind:value={newCategory}
      placeholder="New category name"
    />
    <input
      type="text"
      bind:value={newEmoji}
      placeholder="Emoji (e.g., 🏎️)"
    />
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

  .category-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin-bottom: 1rem;
  }

  .category-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem;
    background: #1a1a25;
    border-radius: 4px;
  }

  .category-name {
    flex: 1;
    color: #e0e0e0;
  }

  .emoji-input {
    width: 80px;
    padding: 0.375rem;
    background: #252535;
    border: 1px solid #353545;
    border-radius: 4px;
    color: #e0e0e0;
    text-align: center;
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

  .add-form input {
    flex: 1;
    padding: 0.5rem;
    background: #1a1a25;
    border: 1px solid #353545;
    border-radius: 4px;
    color: #e0e0e0;
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
