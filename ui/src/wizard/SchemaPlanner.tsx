import { useState, useCallback } from 'react'
import { Network, ArrowLeft, ArrowRight, Save, CheckCircle } from 'lucide-react'
import type { Blueprint } from './data/blueprints'
import type { Dimensions } from './steps/DimensionsStep'
import type { SchemaNode } from './utils/cidr'
import TemplateStep from './steps/TemplateStep'
import StrategyStep from './steps/StrategyStep'
import DimensionsStep from './steps/DimensionsStep'
import PreviewStep from './steps/PreviewStep'
import { useSchemaGenerator } from './hooks/useSchemaGenerator'
import { useConflictChecker } from './hooks/useConflictChecker'
import { useApplySchema } from './hooks/useApplySchema'

const STEPS = [
  { name: 'Template', description: 'Choose starting point' },
  { name: 'Strategy', description: 'Allocation approach' },
  { name: 'Dimensions', description: 'Define boundaries' },
  { name: 'Preview', description: 'Review & apply' },
]

const DRAFT_KEY = 'cloudpam-schema-draft'

function loadDraft(): { blueprint: Blueprint | null; customCidr: string; strategy: string; dimensions: Dimensions } | null {
  try {
    const raw = localStorage.getItem(DRAFT_KEY)
    if (!raw) return null
    return JSON.parse(raw)
  } catch {
    return null
  }
}

function saveDraft(data: { blueprint: Blueprint | null; customCidr: string; strategy: string; dimensions: Dimensions }) {
  localStorage.setItem(DRAFT_KEY, JSON.stringify(data))
}

function clearDraft() {
  localStorage.removeItem(DRAFT_KEY)
}

export default function SchemaPlanner() {
  const draft = loadDraft()

  const [currentStep, setCurrentStep] = useState(0)
  const [selectedBlueprint, setSelectedBlueprint] = useState<Blueprint | null>(draft?.blueprint ?? null)
  const [customCidr, setCustomCidr] = useState(draft?.customCidr ?? '')
  const [strategy, setStrategy] = useState(draft?.strategy ?? 'region-first')
  const [dimensions, setDimensions] = useState<Dimensions>(
    draft?.dimensions ?? {
      regions: ['us-east-1', 'us-west-2', 'eu-west-1'],
      environments: ['prod', 'staging', 'dev'],
      accountTiers: ['core', 'workload'],
      accountsPerEnv: 5,
      growthYears: 3,
    },
  )

  const generatedSchema = useSchemaGenerator({ selectedBlueprint, customCidr, strategy, dimensions })
  const { conflicts, loading: conflictsLoading, error: conflictsError } = useConflictChecker(
    generatedSchema,
    currentStep === 3,
  )
  const { result: applyResult, loading: applyLoading, error: applyError, apply } = useApplySchema()

  const canProceed = () => {
    switch (currentStep) {
      case 0:
        return selectedBlueprint !== null && (selectedBlueprint.id !== 'custom' || customCidr)
      case 1:
        return strategy !== null
      case 2:
        return dimensions.environments.length > 0
      case 3:
        return true
      default:
        return false
    }
  }

  const handleSaveDraft = useCallback(() => {
    saveDraft({ blueprint: selectedBlueprint, customCidr, strategy, dimensions })
  }, [selectedBlueprint, customCidr, strategy, dimensions])

  const handleApply = useCallback(async () => {
    await apply(generatedSchema)
    clearDraft()
  }, [apply, generatedSchema])

  const handleExport = useCallback(
    (format: 'csv' | 'terraform') => {
      const flatten = (node: SchemaNode, parentName: string | null): string[][] => {
        const row = [node.name, node.cidr, node.type, parentName ?? '']
        const rows = [row]
        for (const child of node.children) {
          rows.push(...flatten(child, node.name))
        }
        return rows
      }

      if (format === 'csv') {
        const rows = flatten(generatedSchema, null)
        const csv = ['name,cidr,type,parent_name', ...rows.map((r) => r.join(','))].join('\n')
        const blob = new Blob([csv], { type: 'text/csv' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = 'cloudpam-schema.csv'
        a.click()
        URL.revokeObjectURL(url)
      } else {
        // Terraform placeholder
        const tf = `# CloudPAM Schema - Terraform Export\n# Generated ${new Date().toISOString()}\n# TODO: Implement Terraform export`
        const blob = new Blob([tf], { type: 'text/plain' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = 'cloudpam-schema.tf'
        a.click()
        URL.revokeObjectURL(url)
      }
    },
    [generatedSchema],
  )

  if (applyResult) {
    return (
      <div className="min-h-screen bg-gray-100 dark:bg-gray-900">
        <header className="bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 px-6 py-4">
          <div className="max-w-5xl mx-auto flex items-center gap-3">
            <Network className="w-8 h-8 text-blue-600 dark:text-blue-400" />
            <div>
              <h1 className="text-xl font-bold text-gray-900 dark:text-gray-100">CloudPAM</h1>
              <p className="text-sm text-gray-500 dark:text-gray-400">IP Schema Planner</p>
            </div>
          </div>
        </header>
        <main className="max-w-5xl mx-auto py-8 px-6">
          <div className="bg-white dark:bg-gray-800 rounded-xl shadow-sm border border-gray-200 dark:border-gray-700 p-8 text-center">
            <div className="w-16 h-16 bg-green-100 dark:bg-green-900 rounded-full flex items-center justify-center mx-auto mb-4">
              <CheckCircle className="w-8 h-8 text-green-600 dark:text-green-400" />
            </div>
            <h2 className="text-2xl font-bold text-gray-900 dark:text-gray-100 mb-2">Schema Applied Successfully</h2>
            <p className="text-gray-600 dark:text-gray-300 mb-6">
              Created {applyResult.created} pools. Root pool ID: {applyResult.root_pool_id}
            </p>
            {applyResult.errors.length > 0 && (
              <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-lg p-4 mb-6 text-left">
                <p className="font-medium text-yellow-800 dark:text-yellow-300">Some items had issues:</p>
                <ul className="mt-2 text-sm text-yellow-700 dark:text-yellow-300 list-disc ml-4">
                  {applyResult.errors.map((e, i) => (
                    <li key={i}>{e}</li>
                  ))}
                </ul>
              </div>
            )}
            <div className="flex gap-3 justify-center">
              <a
                href="/"
                className="px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700"
              >
                View Pools Dashboard
              </a>
              <button
                onClick={() => window.location.reload()}
                className="px-6 py-2 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700/50"
              >
                Plan Another Schema
              </button>
            </div>
          </div>
        </main>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-100 dark:bg-gray-900">
      {/* Header */}
      <header className="bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 px-6 py-4">
        <div className="max-w-5xl mx-auto flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Network className="w-8 h-8 text-blue-600 dark:text-blue-400" />
            <div>
              <h1 className="text-xl font-bold text-gray-900 dark:text-gray-100">CloudPAM</h1>
              <p className="text-sm text-gray-500 dark:text-gray-400">IP Schema Planner</p>
            </div>
          </div>
          <a href="/" className="text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 text-sm">
            Back to Dashboard
          </a>
        </div>
      </header>

      <main className="max-w-5xl mx-auto py-8 px-6">
        {/* Progress Steps */}
        <nav className="mb-8">
          <ol className="flex items-center justify-between">
            {STEPS.map((step, i) => (
              <li key={i} className="flex items-center">
                <div className={`flex items-center ${i < STEPS.length - 1 ? 'flex-1' : ''}`}>
                  <div
                    className={`flex items-center justify-center w-10 h-10 rounded-full border-2 ${
                      i < currentStep
                        ? 'bg-blue-600 border-blue-600 text-white'
                        : i === currentStep
                          ? 'border-blue-600 dark:border-blue-500 text-blue-600 dark:text-blue-400'
                          : 'border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
                    }`}
                  >
                    {i < currentStep ? (
                      <CheckCircle className="w-5 h-5" />
                    ) : (
                      <span className="text-sm font-medium">{i + 1}</span>
                    )}
                  </div>
                  <div className="ml-3">
                    <p className={`text-sm font-medium ${i <= currentStep ? 'text-gray-900 dark:text-gray-100' : 'text-gray-400 dark:text-gray-500'}`}>
                      {step.name}
                    </p>
                    <p className="text-xs text-gray-500 dark:text-gray-400">{step.description}</p>
                  </div>
                </div>
                {i < STEPS.length - 1 && (
                  <div className={`w-24 h-0.5 mx-4 ${i < currentStep ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-600'}`} />
                )}
              </li>
            ))}
          </ol>
        </nav>

        {/* Step Content */}
        <div className="bg-white dark:bg-gray-800 rounded-xl shadow-sm border border-gray-200 dark:border-gray-700 p-6 mb-6">
          {currentStep === 0 && (
            <TemplateStep
              selectedBlueprint={selectedBlueprint}
              setSelectedBlueprint={setSelectedBlueprint}
              customCidr={customCidr}
              setCustomCidr={setCustomCidr}
            />
          )}
          {currentStep === 1 && <StrategyStep strategy={strategy} setStrategy={setStrategy} />}
          {currentStep === 2 && (
            <DimensionsStep dimensions={dimensions} setDimensions={setDimensions} strategy={strategy} />
          )}
          {currentStep === 3 && (
            <PreviewStep
              schema={generatedSchema}
              conflicts={conflicts}
              conflictsLoading={conflictsLoading}
              conflictsError={conflictsError}
              onExport={handleExport}
            />
          )}
        </div>

        {/* Navigation */}
        <div className="flex items-center justify-between">
          <button
            onClick={() => setCurrentStep((prev) => prev - 1)}
            disabled={currentStep === 0}
            className="flex items-center gap-2 px-4 py-2 text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <ArrowLeft className="w-4 h-4" />
            Back
          </button>

          <div className="flex items-center gap-3">
            {currentStep === 3 ? (
              <>
                <button
                  onClick={handleSaveDraft}
                  className="flex items-center gap-2 px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700/50"
                >
                  <Save className="w-4 h-4" />
                  Save Draft
                </button>
                <button
                  onClick={handleApply}
                  disabled={applyLoading}
                  className="flex items-center gap-2 px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
                >
                  {applyLoading ? 'Applying...' : 'Apply to CloudPAM'}
                  <ArrowRight className="w-4 h-4" />
                </button>
              </>
            ) : (
              <button
                onClick={() => setCurrentStep((prev) => prev + 1)}
                disabled={!canProceed()}
                className="flex items-center gap-2 px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Continue
                <ArrowRight className="w-4 h-4" />
              </button>
            )}
          </div>
        </div>

        {applyError && (
          <div className="mt-4 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg p-4 text-sm text-red-700 dark:text-red-300">
            Failed to apply schema: {applyError}
          </div>
        )}
      </main>
    </div>
  )
}
