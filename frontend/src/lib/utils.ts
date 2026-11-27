import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function cleanMigrationName(name: string) {
  return name.replace(/^(?:(?:Rollback|Forward):\s*)+/i, '').trim() || name
}
