// Settings panel extracted from settings.tsx. See app/ui/AGENTS.md.

import { useEffect, useMemo, useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { defaultGeneralSettings, loadGeneralSettings, runWorkspaceCleanup, saveGeneralSettings, type GeneralSettingsData } from "@/data/general-settings"
import { docRegistryBase } from "@/data/model-settings"
import { type SettingsSaveStatus } from "./shared"

function sameGeneralSettings(left: GeneralSettingsData, right: GeneralSettingsData): boolean {
  return left["governance.auto_archive_on_delivery_pass"] === right["governance.auto_archive_on_delivery_pass"] &&
    left["retention.artifact_sweep_enabled"] === right["retention.artifact_sweep_enabled"]
}

export function GeneralSettingsPanel({
  workspaceId,
  onSaveStatusChange,
}: {
  workspaceId?: string
  onSaveStatusChange: (status: SettingsSaveStatus) => void
}) {
  const base = useMemo(() => docRegistryBase(), [])
  const [settings, setSettings] = useState<GeneralSettingsData>(() => ({ ...defaultGeneralSettings }))
  const [savedSettings, setSavedSettings] = useState<GeneralSettingsData>(() => ({ ...defaultGeneralSettings }))
  const [loading, setLoading] = useState(Boolean(base))
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [confirmingCleanup, setConfirmingCleanup] = useState(false)
  const [cleaningUp, setCleaningUp] = useState(false)
  const dirty = !sameGeneralSettings(settings, savedSettings)

  useEffect(() => {
    onSaveStatusChange({ canSave: Boolean(base) && dirty && !loading && !saving, saving })
  }, [base, dirty, loading, onSaveStatusChange, saving])

  useEffect(() => {
    if (!base) {
      setLoading(false)
      return
    }

    let cancelled = false
    setLoading(true)
    setError(null)
    loadGeneralSettings(base)
      .then((loaded) => {
        if (!cancelled) {
          setSettings(loaded)
          setSavedSettings(loaded)
        }
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "Failed to load general settings")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [base])

  async function saveSettings() {
    if (!base || !dirty) return
    setSaving(true)
    setMessage(null)
    setError(null)
    try {
      const saved = await saveGeneralSettings(base, settings)
      setSettings(saved)
      setSavedSettings(saved)
      setMessage("General settings saved.")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save general settings")
    } finally {
      setSaving(false)
    }
  }

  async function cleanUpWorkspace() {
    const selectedWorkspace = workspaceId?.trim()
    if (!base || !selectedWorkspace || cleaningUp) return
    setCleaningUp(true)
    setMessage(null)
    setError(null)
    try {
      const counts = await runWorkspaceCleanup(base, selectedWorkspace)
      setConfirmingCleanup(false)
      setMessage(
        `Cleanup complete: ${counts.expired_artifacts_deleted} expired artifacts deleted (${counts.referenced_skipped} kept, still referenced), ` +
          `${counts.archived_change_requests_deleted} archived work items purged, ` +
          `demo seed data removed (${counts.demo_features_deleted} features, ${counts.demo_change_requests_deleted} work items, ${counts.demo_artifacts_deleted} artifacts).`,
      )
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Workspace cleanup failed")
    } finally {
      setCleaningUp(false)
    }
  }

  return (
    <form
      id="general-settings-form"
      className="grid gap-5"
      onSubmit={(event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()
        void saveSettings()
      }}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">General settings</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            Workspace retention and completed-work defaults.
          </p>
        </div>
      </div>

      {!base ? (
        <div className="rounded-lg border border-dashed bg-card/60 p-3 text-sm text-muted-foreground">
          Set <code>VITE_DOC_REGISTRY_URL</code> to load and save General settings.
        </div>
      ) : null}
      {error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-300">
          {error}
        </div>
      ) : null}
      {message ? (
        <div className="rounded-lg border border-green-500/30 bg-green-500/10 p-3 text-sm text-green-700 dark:text-green-300">
          {message}
        </div>
      ) : null}

      <div className="grid gap-3">
        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <label htmlFor="auto-archive-delivery-pass" className="text-sm font-semibold">
                Auto-archive after human delivery acceptance
              </label>
              <p className="mt-1 text-sm text-muted-foreground">
                Hide completed work from the default board after a human accepts delivery.
              </p>
            </div>
            <Switch
              id="auto-archive-delivery-pass"
              checked={settings["governance.auto_archive_on_delivery_pass"] === "true"}
              disabled={loading}
              onCheckedChange={(checked) => {
                setMessage(null)
                setSettings((current) => ({
                  ...current,
                  "governance.auto_archive_on_delivery_pass": checked ? "true" : "false",
                }))
              }}
            />
          </div>
        </div>

        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <label htmlFor="artifact-retention-sweeper" className="text-sm font-semibold">
                Artifact retention sweeper
              </label>
              <p className="mt-1 text-sm text-muted-foreground">
                Daily sweep deletes superseded artifacts older than 90 days and needs-changes artifacts older than 30 days.
                Approved and draft artifacts, and anything a feature or work item still references, are never deleted.
              </p>
            </div>
            <Switch
              id="artifact-retention-sweeper"
              checked={settings["retention.artifact_sweep_enabled"] === "true"}
              disabled={loading}
              onCheckedChange={(checked) => {
                setMessage(null)
                setSettings((current) => ({
                  ...current,
                  "retention.artifact_sweep_enabled": checked ? "true" : "false",
                }))
              }}
            />
          </div>
        </div>

        <div className="rounded-lg border border-red-500/40 bg-red-500/5 p-4">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <span className="text-sm font-semibold text-red-700 dark:text-red-300">Clean up workspace</span>
              <p className="mt-1 text-sm text-muted-foreground">
                Runs the retention sweep now, removes demo seed data, and permanently deletes archived work items.
                Approved artifacts, active features, in-flight work, and audit events are kept.
              </p>
            </div>
            {!confirmingCleanup ? (
              <Button
                type="button"
                variant="destructive"
                size="sm"
                className="rounded-md"
                disabled={!base || !workspaceId?.trim() || cleaningUp}
                onClick={() => {
                  setMessage(null)
                  setConfirmingCleanup(true)
                }}
              >
                Clean up workspace
              </Button>
            ) : null}
          </div>
          {confirmingCleanup ? (
            <div className="mt-3 flex flex-wrap items-center gap-3 rounded-md border border-red-500/40 bg-background/80 p-3">
              <p className="text-sm text-red-700 dark:text-red-300">
                This permanently deletes expired artifacts, demo seed data, and archived work items. It cannot be undone.
              </p>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="destructive"
                  size="sm"
                  className="rounded-md"
                  disabled={cleaningUp}
                  onClick={() => void cleanUpWorkspace()}
                >
                  {cleaningUp ? "Cleaning up…" : "Yes, delete clutter"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="rounded-md"
                  disabled={cleaningUp}
                  onClick={() => setConfirmingCleanup(false)}
                >
                  Cancel cleanup
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </form>
  )
}
