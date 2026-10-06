// VPN20 shield and geometric 20 monogram.
// Keep this drawing aligned with docs/brand/generate.py.
export function Mark({ size = 32 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 128 128" role="img" aria-label="VPN20">
      <path d="M64 6 L114 24 V62 L102 88 L64 122 L26 88 L14 62 V24 Z" fill="#46cac3" stroke="#17404f" strokeWidth="6" strokeLinejoin="round" />
      <path d="M36 50 V42 H58 V58 L36 80 H58 M73 42 H94 V80 H73 Z" fill="none" stroke="#17404f" strokeWidth="8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
