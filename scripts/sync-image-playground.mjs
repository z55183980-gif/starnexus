/*
Copyright (C) 2023-2026 QuantumNous

Sync GPT Image Playground static assets into web/default/public/image-playground.

Usage (from web/default):
  npm run image-playground:sync

Optional custom source path:
  npm run image-playground:sync -- ../../other-path
*/
import { cpSync, existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(__dirname, '..')
const defaultSource = path.join(repoRoot, 'web', 'gpt-image-playground')
const sourceRoot = path.resolve(process.argv[2] || defaultSource)
const targetDir = path.join(repoRoot, 'web', 'default', 'public', 'image-playground')
const envLocalPath = path.join(sourceRoot, '.env.local')

if (!existsSync(path.join(sourceRoot, 'package.json'))) {
  console.error(`GPT Image Playground not found at: ${sourceRoot}`)
  process.exit(1)
}

if (!existsSync(path.join(sourceRoot, 'node_modules'))) {
  console.log('Installing GPT Image Playground dependencies...')
  const install = spawnSync('npm', ['install'], {
    cwd: sourceRoot,
    stdio: 'inherit',
    shell: true,
  })
  if (install.status !== 0) {
    process.exit(install.status ?? 1)
  }
}

writeFileSync(
  envLocalPath,
  `# Generated for StarNexus integration
VITE_DEFAULT_API_URL=/v1
VITE_SHOW_DEFAULT_CONFIG_ONLY=true
VITE_API_PROXY_AVAILABLE=false
`,
  'utf8',
)

console.log(`Building GPT Image Playground from ${sourceRoot}...`)
const build = spawnSync('npm', ['run', 'build'], {
  cwd: sourceRoot,
  stdio: 'inherit',
  shell: true,
})

if (build.status !== 0) {
  process.exit(build.status ?? 1)
}

const distDir = path.join(sourceRoot, 'dist')
if (!existsSync(distDir)) {
  console.error(`Build output missing: ${distDir}`)
  process.exit(1)
}

mkdirSync(path.dirname(targetDir), { recursive: true })
if (existsSync(targetDir)) {
  rmSync(targetDir, { recursive: true, force: true })
}

cpSync(distDir, targetDir, { recursive: true })
console.log(`Synced image playground to ${targetDir}`)
