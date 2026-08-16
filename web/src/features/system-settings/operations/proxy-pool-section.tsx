/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api } from '@/lib/api'

import { SettingsSection } from '../components/settings-section'

type ProxyPoolSettings = {
  enabled: boolean
  default_group: string
  strategy: string
  circuit_breaker_threshold: number
  health_check_interval: number
  max_concurrent_per_proxy: number
}

type ProxyPoolEntry = {
  id: number
  name: string
  type: string
  host: string
  port: number
  username: string
  password?: string
  weight: number
  enabled: boolean
  group_name: string
  status: string
  fail_count: number
  success_count: number
  last_check?: string
  last_used?: string
  masked_url?: string
}

type EntryForm = Pick<
  ProxyPoolEntry,
  | 'name'
  | 'type'
  | 'host'
  | 'port'
  | 'username'
  | 'weight'
  | 'enabled'
  | 'group_name'
> & { password: string }

const defaultSettings: ProxyPoolSettings = {
  enabled: false,
  default_group: 'default',
  strategy: 'weighted_round_robin',
  circuit_breaker_threshold: 5,
  health_check_interval: 60,
  max_concurrent_per_proxy: 10,
}

const emptyEntry: EntryForm = {
  name: '',
  type: 'http',
  host: '',
  port: 8080,
  username: '',
  password: '',
  weight: 1,
  enabled: true,
  group_name: 'default',
}

function errorMessage(error: unknown): string {
  if (
    typeof error === 'object' &&
    error !== null &&
    'response' in error &&
    typeof error.response === 'object' &&
    error.response !== null &&
    'data' in error.response
  ) {
    const data = error.response.data as { message?: string }
    if (data.message) return data.message
  }
  return error instanceof Error ? error.message : 'Request failed'
}

function StatusBadge({
  status,
  enabled,
}: {
  status: string
  enabled: boolean
}) {
  if (!enabled) return <Badge variant='secondary'>Disabled</Badge>
  if (status === 'healthy') return <Badge variant='default'>Healthy</Badge>
  if (status === 'unhealthy')
    return <Badge variant='destructive'>Unhealthy</Badge>
  return <Badge variant='outline'>Unknown</Badge>
}

function EntryDialog({
  open,
  entry,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  entry: ProxyPoolEntry | null
  onOpenChange: (open: boolean) => void
  onSaved: () => Promise<void>
}) {
  const [form, setForm] = useState<EntryForm>(emptyEntry)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    setForm(
      entry
        ? {
            name: entry.name,
            type: entry.type,
            host: entry.host,
            port: entry.port,
            username: entry.username ?? '',
            password: '',
            weight: entry.weight,
            enabled: entry.enabled,
            group_name: entry.group_name || 'default',
          }
        : emptyEntry
    )
  }, [entry, open])

  const save = async () => {
    if (!form.name.trim() || !form.host.trim()) {
      toast.error('Name and host are required')
      return
    }
    if (form.port < 1 || form.port > 65535) {
      toast.error('Port must be between 1 and 65535')
      return
    }
    setSaving(true)
    try {
      const payload = { ...form }
      if (entry && !payload.password)
        delete (payload as Partial<EntryForm>).password
      const response = entry
        ? await api.put(`/api/proxy-pool/${entry.id}`, payload)
        : await api.post('/api/proxy-pool/', payload)
      if (!response.data.success) throw new Error(response.data.message)
      toast.success(entry ? 'Proxy updated' : 'Proxy created')
      onOpenChange(false)
      await onSaved()
    } catch (error) {
      toast.error(errorMessage(error))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{entry ? 'Edit proxy' : 'Add proxy'}</DialogTitle>
          <DialogDescription>
            {entry
              ? 'Stored passwords are never displayed. Leave password blank to keep the current value.'
              : 'Add a proxy endpoint to the system-wide pool.'}
          </DialogDescription>
        </DialogHeader>
        <div className='grid gap-4 sm:grid-cols-2'>
          <div className='grid gap-2'>
            <Label htmlFor='proxy-name'>Name</Label>
            <Input
              id='proxy-name'
              value={form.name}
              onChange={(event) =>
                setForm({ ...form, name: event.target.value })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label>Type</Label>
            <Select
              value={form.type}
              onValueChange={(value) =>
                setForm({ ...form, type: value ?? 'http' })
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {['http', 'https', 'socks5', 'socks5h'].map((type) => (
                  <SelectItem key={type} value={type}>
                    {type}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='proxy-host'>Host</Label>
            <Input
              id='proxy-host'
              value={form.host}
              onChange={(event) =>
                setForm({ ...form, host: event.target.value })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='proxy-port'>Port</Label>
            <Input
              id='proxy-port'
              type='number'
              min={1}
              max={65535}
              value={form.port}
              onChange={(event) =>
                setForm({ ...form, port: Number(event.target.value) })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='proxy-username'>Username</Label>
            <Input
              id='proxy-username'
              autoComplete='off'
              value={form.username}
              onChange={(event) =>
                setForm({ ...form, username: event.target.value })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='proxy-password'>Password</Label>
            <Input
              id='proxy-password'
              type='password'
              autoComplete='new-password'
              placeholder={entry ? 'Leave blank to keep existing password' : ''}
              value={form.password}
              onChange={(event) =>
                setForm({ ...form, password: event.target.value })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='proxy-group'>Group</Label>
            <Input
              id='proxy-group'
              value={form.group_name}
              onChange={(event) =>
                setForm({ ...form, group_name: event.target.value })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='proxy-weight'>Weight</Label>
            <Input
              id='proxy-weight'
              type='number'
              min={1}
              value={form.weight}
              onChange={(event) =>
                setForm({ ...form, weight: Number(event.target.value) })
              }
            />
          </div>
          <div className='flex items-center justify-between gap-4 rounded-lg border p-3 sm:col-span-2'>
            <div>
              <Label>Enabled</Label>
              <p className='text-muted-foreground text-sm'>
                Allow this endpoint to receive requests.
              </p>
            </div>
            <Switch
              checked={form.enabled}
              onCheckedChange={(enabled) => setForm({ ...form, enabled })}
            />
          </div>
        </div>
        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={saving}
          >
            Cancel
          </Button>
          <Button onClick={save} disabled={saving}>
            {saving ? 'Saving...' : 'Save'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function ProxyPoolSection() {
  const [settings, setSettings] = useState(defaultSettings)
  const [entries, setEntries] = useState<ProxyPoolEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [savingSettings, setSavingSettings] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<ProxyPoolEntry | null>(null)
  const [deleting, setDeleting] = useState<ProxyPoolEntry | null>(null)
  const [checking, setChecking] = useState<number | 'batch' | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [settingsResponse, entriesResponse] = await Promise.all([
        api.get('/api/proxy-pool/settings'),
        api.get('/api/proxy-pool/'),
      ])
      if (!settingsResponse.data.success)
        throw new Error(settingsResponse.data.message)
      if (!entriesResponse.data.success)
        throw new Error(entriesResponse.data.message)
      setSettings(settingsResponse.data.data)
      setEntries(entriesResponse.data.data ?? [])
    } catch (requestError) {
      setError(errorMessage(requestError))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const saveSettings = async () => {
    setSavingSettings(true)
    try {
      const response = await api.post('/api/proxy-pool/settings', settings)
      if (!response.data.success) throw new Error(response.data.message)
      toast.success('Proxy pool settings saved')
    } catch (requestError) {
      toast.error(errorMessage(requestError))
    } finally {
      setSavingSettings(false)
    }
  }

  const checkHealth = async (entry?: ProxyPoolEntry) => {
    setChecking(entry?.id ?? 'batch')
    try {
      const response = entry
        ? await api.post(`/api/proxy-pool/${entry.id}/health`)
        : await api.post('/api/proxy-pool/health/batch', null, {
            params: { group: settings.default_group || 'default' },
          })
      if (!response.data.success) throw new Error(response.data.message)
      toast.success(
        entry ? response.data.message : 'Batch health check completed'
      )
      await load()
    } catch (requestError) {
      toast.error(errorMessage(requestError))
    } finally {
      setChecking(null)
    }
  }

  const deleteEntry = async () => {
    if (!deleting) return
    try {
      const response = await api.delete(`/api/proxy-pool/${deleting.id}`)
      if (!response.data.success) throw new Error(response.data.message)
      toast.success('Proxy deleted')
      setDeleting(null)
      await load()
    } catch (requestError) {
      toast.error(errorMessage(requestError))
    }
  }

  if (loading && entries.length === 0) {
    return (
      <div className='space-y-4'>
        <Skeleton className='h-40 w-full' />
        <Skeleton className='h-64 w-full' />
      </div>
    )
  }

  return (
    <SettingsSection title='Proxy Pool' className='gap-6'>
      {error && (
        <Alert variant='destructive'>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Pool settings</CardTitle>
          <CardDescription>
            Configure routing, health checks, circuit breaking, and concurrency.
          </CardDescription>
        </CardHeader>
        <CardContent className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
          <div className='flex items-center justify-between gap-4 rounded-lg border p-3 sm:col-span-2 lg:col-span-3'>
            <div>
              <Label>Enable proxy pool</Label>
              <p className='text-muted-foreground text-sm'>
                Route eligible requests through configured proxies.
              </p>
            </div>
            <Switch
              checked={settings.enabled}
              onCheckedChange={(enabled) =>
                setSettings({ ...settings, enabled })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label>Default group</Label>
            <Input
              value={settings.default_group}
              onChange={(event) =>
                setSettings({ ...settings, default_group: event.target.value })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label>Strategy</Label>
            <Select
              value={settings.strategy}
              onValueChange={(strategy) =>
                setSettings({
                  ...settings,
                  strategy: strategy ?? 'weighted_round_robin',
                })
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='weighted_round_robin'>
                  Weighted round robin
                </SelectItem>
                <SelectItem value='round_robin'>Round robin</SelectItem>
                <SelectItem value='random'>Random</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className='grid gap-2'>
            <Label>Circuit-breaker failures</Label>
            <Input
              type='number'
              min={1}
              value={settings.circuit_breaker_threshold}
              onChange={(event) =>
                setSettings({
                  ...settings,
                  circuit_breaker_threshold: Number(event.target.value),
                })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label>Health interval (seconds)</Label>
            <Input
              type='number'
              min={10}
              value={settings.health_check_interval}
              onChange={(event) =>
                setSettings({
                  ...settings,
                  health_check_interval: Number(event.target.value),
                })
              }
            />
          </div>
          <div className='grid gap-2'>
            <Label>Max concurrent per proxy</Label>
            <Input
              type='number'
              min={1}
              value={settings.max_concurrent_per_proxy}
              onChange={(event) =>
                setSettings({
                  ...settings,
                  max_concurrent_per_proxy: Number(event.target.value),
                })
              }
            />
          </div>
          <div className='flex items-end'>
            <Button onClick={saveSettings} disabled={savingSettings}>
              {savingSettings ? 'Saving...' : 'Save settings'}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className='grid-cols-[1fr_auto]'>
          <div>
            <CardTitle>Proxy entries</CardTitle>
            <CardDescription>
              {entries.length} configured endpoint
              {entries.length === 1 ? '' : 's'}.
            </CardDescription>
          </div>
          <div className='flex flex-wrap justify-end gap-2'>
            <Button
              variant='outline'
              onClick={() => void load()}
              disabled={loading}
            >
              Refresh
            </Button>
            <Button
              variant='outline'
              onClick={() => void checkHealth()}
              disabled={checking !== null}
            >
              {checking === 'batch' ? 'Checking...' : 'Check group'}
            </Button>
            <Button
              onClick={() => {
                setEditing(null)
                setDialogOpen(true)
              }}
            >
              Add proxy
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <div className='overflow-x-auto rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Endpoint</TableHead>
                  <TableHead>Group</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Failures / successes</TableHead>
                  <TableHead className='text-right'>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {entries.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className='text-muted-foreground h-24 text-center'
                    >
                      No proxy entries configured.
                    </TableCell>
                  </TableRow>
                ) : (
                  entries.map((entry) => (
                    <TableRow key={entry.id}>
                      <TableCell className='font-medium'>
                        {entry.name}
                      </TableCell>
                      <TableCell>
                        <code className='text-xs'>
                          {entry.masked_url ||
                            `${entry.type}://${entry.host}:${entry.port}`}
                        </code>
                      </TableCell>
                      <TableCell>
                        {entry.group_name || 'default'} · weight {entry.weight}
                      </TableCell>
                      <TableCell>
                        <StatusBadge
                          status={entry.status}
                          enabled={entry.enabled}
                        />
                      </TableCell>
                      <TableCell>
                        {entry.fail_count} / {entry.success_count}
                      </TableCell>
                      <TableCell>
                        <div className='flex justify-end gap-2'>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => void checkHealth(entry)}
                            disabled={checking !== null}
                          >
                            {checking === entry.id ? 'Checking...' : 'Check'}
                          </Button>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => {
                              setEditing(entry)
                              setDialogOpen(true)
                            }}
                          >
                            Edit
                          </Button>
                          <Button
                            size='sm'
                            variant='destructive'
                            onClick={() => setDeleting(entry)}
                          >
                            Delete
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      <EntryDialog
        open={dialogOpen}
        entry={editing}
        onOpenChange={setDialogOpen}
        onSaved={load}
      />
      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete proxy?</AlertDialogTitle>
            <AlertDialogDescription>
              This permanently removes {deleting?.name}. Existing traffic will
              no longer use this endpoint.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => void deleteEntry()}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
