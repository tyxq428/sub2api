import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../VersionBadge.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('VersionBadge build identity', () => {
  it('renders build identity next to semantic version without replacing it', () => {
    expect(componentSource).toContain('v{{ currentVersion }}')
    expect(componentSource).toContain('· {{ buildIdentity }}')
    expect(componentSource).toContain('appStore.buildLabel.trim()')
    expect(componentSource).toContain('appStore.buildCommit.trim()')
    expect(componentSource).toContain('commit.slice(0, 8)')
  })
})
