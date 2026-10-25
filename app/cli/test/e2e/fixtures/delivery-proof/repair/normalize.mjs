export function normalizeName(name) {
  const normalized = name.trim().toLowerCase();
  if (!normalized) throw new Error('Name must not be empty');
  return normalized;
}
