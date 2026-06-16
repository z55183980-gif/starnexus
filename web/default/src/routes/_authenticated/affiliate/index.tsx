import { createFileRoute } from '@tanstack/react-router'
import { AffiliateProgram } from '@/features/affiliate'

export const Route = createFileRoute('/_authenticated/affiliate/')({
  component: AffiliateProgram,
})
