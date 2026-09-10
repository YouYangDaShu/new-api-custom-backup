export const SMART_ROUTE_GROUPS = [
  {
    value: "@smart:balanced",
    label: "Smart balanced",
    desc: "Balances price, success rate, and latency",
  },
  {
    value: "@smart:price",
    label: "Smart lowest price",
    desc: "Prefers the lowest-priced available group",
  },
  {
    value: "@smart:success_rate",
    label: "Smart highest success rate",
    desc: "Prefers the most reliable available group",
  },
] as const;

export function isSmartTokenGroup(value?: string | null): boolean {
  return Boolean(value?.trim().startsWith("@smart:"));
}
