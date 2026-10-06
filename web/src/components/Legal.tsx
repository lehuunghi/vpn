// Company identity and source access, shown in navigation and auth pages.
// Upstream copyright and third-party licences remain in the repository NOTICE.
export function Legal({ center = false }: { center?: boolean }) {
  return (
    <p className={`legal${center ? " center" : ""}`}>
      <span>Công ty TNHH TN20</span>
      <span className="legal-sep" aria-hidden="true">·</span>
      <a href="https://github.com/lehuunghi/vpn" target="_blank" rel="noreferrer">
        AGPL-3.0 source
      </a>
      <a href="https://github.com/lehuunghi/vpn/blob/main/NOTICE" target="_blank" rel="noreferrer">
        Licences &amp; credits
      </a>
    </p>
  );
}
