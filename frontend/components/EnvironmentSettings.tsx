import { useState } from "react"
import toast from "react-hot-toast"
import { Loader2, Settings, Save, Trash2, Plus } from "lucide-react"

interface EnvVarRow {
  key: string
  value: string
  isSet: boolean
}

export default function EnvironmentSettings({ env, mutate, isViewerRole }: { env: Record<string, unknown>, mutate: () => void, isViewerRole: boolean }) {
  const [startCommand, setStartCommand] = useState((env.start_command as string) || "")
  const [envVars, setEnvVars] = useState<EnvVarRow[]>(() => {
    const raw = env.env_vars
    if (!Array.isArray(raw)) return []
    return raw.map((v: any) => ({ key: v.key, value: "", isSet: Boolean(v.is_set) }))
  })
  const [isSaving, setIsSaving] = useState(false)

  const handleSave = async () => {
    setIsSaving(true)
    try {
      const res = await fetch(`/api/environments/${env.id}/env`, {
        method: "PUT",
        headers: {
          "Authorization": `Bearer ${localStorage.getItem("token")}`,
          "Content-Type": "application/json"
        },
        body: JSON.stringify({
          start_command: startCommand,
          env_vars: envVars.reduce((acc, curr) => {
            if (curr.key.trim()) acc[curr.key.trim()] = curr.value
            return acc
          }, {} as Record<string, string>)
        })
      })
      const data = await res.json().catch(() => ({}))
      if (res.ok) {
        toast.success(data.message || "Settings saved — redeploy required to apply.")
        mutate()
      } else {
        throw new Error(data.error || "Failed to update settings")
      }
    } catch(err) {
      if (err instanceof Error) {
        toast.error((err as Error).message)
      } else {
        toast.error("An unknown error occurred")
      }
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <div className="bg-surface-container-lowest border border-outline-variant rounded-xl p-6 max-w-3xl">
      <div className="flex items-center gap-2 mb-6">
        <Settings className="w-5 h-5 text-on-surface-variant/70" />
        <h3 className="font-semibold text-lg">Environment Settings</h3>
      </div>
      
      <div className="space-y-6">
        <div>
          <label className="block text-sm font-medium text-on-surface-variant mb-1">Start Command Override</label>
          <input 
            type="text" 
            value={startCommand}
            onChange={e => setStartCommand(e.target.value)}
            disabled={isViewerRole}
            placeholder="e.g. npm run dev"
            className="w-full px-3 py-2 bg-surface-container/50 border border-outline-variant rounded-lg focus:border-primary-fixed focus:ring-1 focus:ring-primary-fixed outline-none transition-all disabled:opacity-50"
          />
          <p className="text-xs text-on-surface-variant/70 mt-1">Leave blank to rely on automatic heuristics.</p>
        </div>

        <div>
          <div className="flex items-center justify-between mb-4">
            <h4 className="text-sm font-medium text-on-surface-variant">Environment Variables</h4>
            {!isViewerRole && (
              <button
                onClick={() => setEnvVars([...envVars, { key: "", value: "", isSet: false }])}
                className="text-xs flex items-center gap-1.5 px-3 py-1.5 bg-surface-container hover:bg-surface-container-high rounded-md transition-colors border border-outline-variant"
              >
                <Plus className="w-3.5 h-3.5" />
                Add Variable
              </button>
            )}
          </div>
          
          <div className="space-y-3">
            {envVars.length === 0 && (
              <p className="text-sm text-on-surface-variant/60 italic border border-dashed border-outline-variant rounded-lg p-4 text-center">
                No environment variables set.
              </p>
            )}
            {envVars.map((v, i) => (
              <div key={i} className="flex items-start gap-3">
                <div className="flex-1">
                  <input
                    type="text"
                    value={v.key}
                    onChange={(e) => {
                      const newVars = [...envVars]
                      newVars[i].key = e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, '')
                      setEnvVars(newVars)
                    }}
                    disabled={isViewerRole}
                    placeholder="NAME_OF_VARIABLE"
                    className="w-full px-3 py-2 bg-surface-container/50 border border-outline-variant rounded-lg focus:border-primary-fixed focus:ring-1 focus:ring-primary-fixed outline-none transition-all disabled:opacity-50 font-mono text-sm"
                  />
                </div>
                <div className="flex-1">
                  <input
                    type="text"
                    value={v.value}
                    onChange={(e) => {
                      const newVars = [...envVars]
                      newVars[i].value = e.target.value
                      setEnvVars(newVars)
                    }}
                    disabled={isViewerRole}
                    placeholder={v.isSet ? "•••••••• (leave blank to keep)" : "value"}
                    className="w-full px-3 py-2 bg-surface-container/50 border border-outline-variant rounded-lg focus:border-primary-fixed focus:ring-1 focus:ring-primary-fixed outline-none transition-all disabled:opacity-50 font-mono text-sm"
                  />
                </div>
                {!isViewerRole && (
                  <button
                    onClick={() => {
                      const newVars = [...envVars]
                      newVars.splice(i, 1)
                      setEnvVars(newVars)
                    }}
                    className="p-2 text-error/70 hover:text-error hover:bg-error/10 rounded-lg transition-colors mt-0.5 shrink-0"
                    title="Remove variable"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                )}
              </div>
            ))}
          </div>
          <p className="text-xs text-on-surface-variant/70 mt-2">Existing secrets are never shown. Leave a value blank to keep the stored value.</p>
        </div>

        {!isViewerRole && (
          <div className="pt-4 border-t border-outline-variant flex items-center justify-between">
            <p className="text-xs text-on-surface-variant/80">Changes apply on the next redeploy.</p>
            <button
              onClick={handleSave}
              disabled={isSaving}
              className="px-4 py-2 bg-primary-fixed text-on-primary-fixed rounded-lg text-sm font-semibold hover:opacity-90 transition-opacity flex items-center gap-2 disabled:opacity-50"
            >
              {isSaving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
              Save Settings
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
