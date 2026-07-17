<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api } from './api'
import type { Project, Seed, SeedStatus, SeedType } from './types'

const types: { value: SeedType | 'all'; label: string; icon: string }[] = [
  { value: 'all', label: '全部类型', icon: '◇' },
  { value: 'idea', label: '灵感', icon: '✦' },
  { value: 'feature', label: '功能', icon: '◈' },
  { value: 'todo', label: '事项', icon: '✓' },
  { value: 'bug', label: 'Bug', icon: '⌁' },
]
const statuses: { value: SeedStatus; label: string }[] = [
  { value: 'inbox', label: '待实现' }, { value: 'planned', label: '已拆分' },
  { value: 'done', label: '已完成' }, { value: 'archived', label: '已废弃' },
]

const projects = ref<Project[]>([])
const seeds = ref<Seed[]>([])
const allSeeds = ref<Seed[]>([])
const projectId = ref<number>(0)
const filter = ref<SeedType | 'all'>('all')
const statusFilter = ref<SeedStatus | 'all'>('all')
const projectDialog = ref(false)
const seedDialog = ref(false)
const editingId = ref<number | null>(null)
const busy = ref(false)
const error = ref('')
const projectForm = reactive({ name: '', description: '' })
const seedForm = reactive({ type: 'todo' as SeedType, status: 'inbox' as SeedStatus, title: '', content: '', priority: 0 })

const currentProject = computed(() => projects.value.find(p => p.id === projectId.value))
const count = (type: SeedType | 'all') => type === 'all' ? allSeeds.value.length : allSeeds.value.filter(s => s.type === type).length
const statusCount = (status: SeedStatus | 'all') => status === 'all' ? allSeeds.value.length : allSeeds.value.filter(s => s.status === status).length

async function loadProjects() {
  try {
    projects.value = await api.projects()
    if (!projectId.value && projects.value.length) projectId.value = projects.value[0].id
    if (!projects.value.length) projectDialog.value = true
  } catch (e) { showError(e) }
}
async function loadSeeds() {
  if (!projectId.value) { seeds.value = []; allSeeds.value = []; return }
  busy.value = true
  try {
    const [items, all] = await Promise.all([
      api.seeds(projectId.value, filter.value, statusFilter.value),
      api.seeds(projectId.value, 'all', 'all'),
    ])
    seeds.value = items
    allSeeds.value = all
  }
  catch (e) { showError(e) }
  finally { busy.value = false }
}
async function createProject() {
  if (!projectForm.name.trim()) return
  try {
    const item = await api.createProject(projectForm)
    projects.value.unshift(item); projectId.value = item.id
    projectForm.name = ''; projectForm.description = ''; projectDialog.value = false
  } catch (e) { showError(e) }
}
function openSeed(seed?: Seed) {
  editingId.value = seed?.id ?? null
  seedForm.type = seed?.type ?? (filter.value === 'all' ? 'todo' : filter.value)
  seedForm.status = seed?.status ?? 'inbox'
  seedForm.title = seed?.title ?? ''
  seedForm.content = seed?.content ?? ''
  seedForm.priority = seed?.priority ?? 0
  seedDialog.value = true
}
async function saveSeed() {
  if (!seedForm.title.trim() || !projectId.value) return
  const value = { id: editingId.value ?? 0, projectId: projectId.value, ...seedForm }
  try {
    if (editingId.value) await api.updateSeed(value)
    else await api.createSeed(value)
    seedDialog.value = false; await loadSeeds()
  } catch (e) { showError(e) }
}
async function updateSeedMeta(seed: Seed, field: "type" | "status", event: Event) {
  const value = (event.target as HTMLSelectElement).value as SeedType | SeedStatus
  const previous = seed[field]
  Object.assign(seed, { [field]: value })
  try {
    await api.updateSeed(seed)
    await loadSeeds()
  } catch (e) {
    Object.assign(seed, { [field]: previous })
    showError(e)
  }
}
async function removeSeed(id: number) {
  if (!confirm('确定删除这颗种子吗？')) return
  try { await api.deleteSeed(id); await loadSeeds() } catch (e) { showError(e) }
}
function showError(value: unknown) { error.value = value instanceof Error ? value.message : '操作失败'; window.setTimeout(() => error.value = '', 3000) }
function typeInfo(value: SeedType) { return types.find(t => t.value === value)! }
function statusLabel(value: SeedStatus) { return statuses.find(s => s.value === value)?.label }

watch([projectId, filter, statusFilter], loadSeeds)
onMounted(loadProjects)
</script>

<template>
  <main class="shell">
    <header class="topbar">
      <div class="brand"><span class="brand-mark">芽</span><div><strong>拾种</strong><small>WORKSEED</small></div><span class="brand-tagline">把工作中的每个念头，都安放在这里。</span></div>
      <div class="project-picker">
        <span class="label">当前项目</span>
        <select v-model="projectId"><option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option></select>
        <button class="quiet" @click="projectDialog = true">＋ 新建项目</button>
      </div>
      <button class="primary" :disabled="!projectId" @click="openSeed()">＋ 播下一颗种子</button>
    </header>

    <section v-if="currentProject?.description" class="hero">
      <p class="eyebrow">{{ currentProject.description }}</p>
      <div class="summary"><strong>{{ seeds.length }}</strong><span>颗种子</span></div>
    </section>

    <div class="filter-row">
      <nav class="filters" aria-label="种子类型">
      <button v-for="item in types" :key="item.value" :class="{ active: filter === item.value }" @click="filter = item.value">
        <span>{{ item.icon }}</span>{{ item.label }}<em>{{ count(item.value) }}</em>
      </button>
    </nav>

      <nav class="filters status-filters" aria-label="种子状态">
      <button :class="{ active: statusFilter === 'all' }" v-on:click="statusFilter = 'all'">全部状态<em>{{ statusCount('all') }}</em></button>
      <button v-for="item in statuses" :key="item.value" :class="{ active: statusFilter === item.value }" v-on:click="statusFilter = item.value">{{ item.label }}<em>{{ statusCount(item.value) }}</em></button>
      </nav>
    </div>

    <section class="seed-list">
      <p v-if="busy" class="empty">正在翻土……</p>
      <div v-else-if="!projectId" class="empty"><b>先创建一个项目</b><span>项目是一片苗圃，用来收纳相关的工作种子。</span></div>
      <div v-else-if="!seeds.length && (filter !== 'all' || statusFilter !== 'all')" class="empty"><b>没有符合条件的种子</b><span>尝试切换类型或状态筛选。</span><button class="quiet" v-on:click="filter = 'all'; statusFilter = 'all'">清除筛选</button></div>
      <div v-else-if="!seeds.length" class="empty"><b>这里还没有种子</b><span>记录一闪而过的灵感，或下一件要完成的事。</span><button class="primary" v-on:click="openSeed()">播下第一颗</button></div>
      <article v-for="seed in seeds" :key="seed.id" class="seed-card" @click="openSeed(seed)">
        <div class="type-dot" :class="seed.type">{{ typeInfo(seed.type).icon }}</div>
        <div class="seed-body"><div class="seed-meta"><select :value="seed.type" aria-label="种子类型" @click.stop @change="updateSeedMeta(seed, 'type', $event)"><option v-for="item in types.slice(1)" :key="item.value" :value="item.value">{{ item.label }}</option></select><i>·</i><select :value="seed.status" aria-label="种子状态" @click.stop @change="updateSeedMeta(seed, 'status', $event)"><option v-for="item in statuses" :key="item.value" :value="item.value">{{ item.label }}</option></select></div><h2>{{ seed.title }}</h2><p v-if="seed.content">{{ seed.content }}</p></div>
        <button class="icon-button" title="删除" @click.stop="removeSeed(seed.id)">×</button>
      </article>
    </section>
  </main>

  <div v-if="projectDialog" class="overlay" @click.self="projectDialog = false">
    <form class="dialog" @submit.prevent="createProject"><button type="button" class="close" @click="projectDialog = false">×</button><p class="eyebrow">新的苗圃</p><h2>创建项目</h2><label>项目名称<input v-model="projectForm.name" autofocus placeholder="例如：Workseed" /></label><label>简单描述<textarea v-model="projectForm.description" rows="3" placeholder="这个项目在做什么？"></textarea></label><div class="actions"><button type="button" class="quiet" @click="projectDialog = false">取消</button><button class="primary">创建项目</button></div></form>
  </div>

  <div v-if="seedDialog" class="overlay" @click.self="seedDialog = false">
    <form class="dialog seed-form" @submit.prevent="saveSeed"><button type="button" class="close" @click="seedDialog = false">×</button><p class="eyebrow">{{ editingId ? '照料种子' : '捕捉此刻' }}</p><h2>{{ editingId ? '编辑种子' : '播下一颗种子' }}</h2><div class="grid"><label>类型<select v-model="seedForm.type"><option v-for="item in types.slice(1)" :key="item.value" :value="item.value">{{ item.label }}</option></select></label><label>状态<select v-model="seedForm.status"><option v-for="item in statuses" :key="item.value" :value="item.value">{{ item.label }}</option></select></label></div><label>标题<input v-model="seedForm.title" autofocus placeholder="一句话记下它" /></label><label>详细内容<textarea v-model="seedForm.content" rows="6" placeholder="背景、想法或验收方式……"></textarea></label><div class="actions"><button type="button" class="quiet" @click="seedDialog = false">取消</button><button class="primary">{{ editingId ? '保存修改' : '播下种子' }}</button></div></form>
  </div>
  <div v-if="error" class="toast">{{ error }}</div>
</template>

