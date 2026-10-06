import type { ReactNode } from "react";
import { Link, useLocation } from "wouter";
import { Activity, ClipboardList, LayoutDashboard, Settings, Users } from "lucide-react";
import { useLive } from "../state";
import { Mark } from "./Mark";
import { ThemeToggle } from "./ThemeSwitch";
import { UserMenu } from "./UserMenu";
import { Legal } from "./Legal";

// Account is reached through the user menu, so it is not a nav item.
const groups = [
  {
    label: "Tổng quan",
    items: [
      { href: "/", label: "Bảng điều khiển", icon: LayoutDashboard },
      { href: "/peers", label: "Thiết bị", icon: Activity },
    ],
  },
  {
    label: "Quản trị",
    items: [
      { href: "/settings", label: "Cài đặt", icon: Settings },
      { href: "/users", label: "Người dùng", icon: Users },
      { href: "/audit", label: "Nhật ký hoạt động", icon: ClipboardList },
    ],
  },
];
const all = groups.flatMap((g) => g.items);

function isActive(href: string, location: string) {
  return href === "/" ? location === "/" : location.startsWith(href);
}

function LivePill() {
  const { connected } = useLive();
  return (
    <span className={`live-pill${connected ? " on" : ""}`} title={connected ? "Đã kết nối cập nhật trực tiếp" : "Đang kết nối lại cập nhật trực tiếp"}>
      <i /> {connected ? "Trực tiếp" : "Đang kết nối lại"}
    </span>
  );
}

export function Layout({ children }: { children: ReactNode }) {
  const [location] = useLocation();
  return (
    <div className="shell">
      {/* Phones and narrow windows: brand and account controls up top. */}
      <header className="topbar">
        <Link href="/" className="brand" aria-label="Bảng điều khiển VPN20">
          <Mark size={30} />
          <span className="brand-name">VPN20</span>
        </Link>
        <div className="topbar-right">
          <LivePill />
          <ThemeToggle />
          <UserMenu placement="down" />
        </div>
      </header>

      {/* Desktop: everything lives in the sidebar. */}
      <aside className="sidebar">
        <Link href="/" className="brand" aria-label="Bảng điều khiển VPN20">
          <Mark size={34} />
          <span>
            <span className="brand-name">VPN20</span>
            <span className="brand-sub">Công ty TNHH TN20</span>
          </span>
        </Link>
        <nav className="nav" aria-label="Điều hướng chính">
          {groups.map((g) => (
            <div className="nav-group" key={g.label}>
              <div className="nav-label">{g.label}</div>
              {g.items.map((it) => {
                const Icon = it.icon;
                return (
                  <Link key={it.href} href={it.href} className={isActive(it.href, location) ? "active" : ""} aria-current={isActive(it.href, location) ? "page" : undefined}>
                    <Icon />
                    <span>{it.label}</span>
                  </Link>
                );
              })}
            </div>
          ))}
        </nav>
        <div className="sidebar-foot">
          <div className="sidebar-tools">
            <LivePill />
            <ThemeToggle />
          </div>
          <UserMenu placement="up" showName />
          <Legal />
        </div>
      </aside>

      <main className="main" id="main">
        {children}
      </main>

      {/* Phones: primary navigation as a tab bar within thumb reach. */}
      <nav className="tabbar" aria-label="Điều hướng chính">
        {all.map((it) => {
          const Icon = it.icon;
          const active = isActive(it.href, location);
          return (
            <Link key={it.href} href={it.href} className={active ? "active" : ""} aria-current={active ? "page" : undefined}>
              <Icon />
              <span>{it.label}</span>
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
