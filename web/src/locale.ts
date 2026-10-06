// Ngôn ngữ mặc định của giao diện VPN20.
export const DEFAULT_LANGUAGE = "vi";
export const DEFAULT_LOCALE = "vi-VN";
export const COMPANY_URL = "https://20.com.vn";

export function number(value: number, digits = 0): string {
  return new Intl.NumberFormat(DEFAULT_LOCALE, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(value);
}

export function roleLabel(role?: string): string {
  return role === "admin" ? "Quản trị viên" : role === "viewer" ? "Người xem" : (role ?? "");
}

export function backendLabel(backend: string): string {
  return ({ kernel: "xử lý trong nhân", userspace: "xử lý trong không gian người dùng", mock: "chế độ mô phỏng" } as Record<string, string>)[backend] ?? backend;
}

const errorMessages: Record<string, string> = {
  "request failed": "Yêu cầu thất bại.",
  "Failed to fetch": "Không thể kết nối đến máy chủ. Hãy kiểm tra mạng và thử lại.",
  "fetch failed": "Không thể kết nối đến máy chủ. Hãy kiểm tra mạng và thử lại.",
  "NetworkError when attempting to fetch resource.": "Không thể kết nối đến máy chủ. Hãy kiểm tra mạng và thử lại.",
  "Load failed": "Không thể tải dữ liệu. Hãy kiểm tra kết nối mạng.",
  "cross-site request refused": "Yêu cầu từ website khác đã bị từ chối.",
  "not signed in": "Bạn chưa đăng nhập hoặc phiên đăng nhập đã hết hạn.",
  "administrator role required": "Thao tác này cần quyền quản trị viên.",
  "setup has already been completed": "Hệ thống đã được thiết lập.",
  "username must be 2-32 characters: letters, digits, dot, dash or underscore": "Tên đăng nhập phải có 2–32 ký tự, gồm chữ cái, chữ số, dấu chấm, gạch ngang hoặc gạch dưới.",
  "too many attempts; try again later": "Bạn đã thử quá nhiều lần. Vui lòng thử lại sau.",
  "wrong username or password": "Tên đăng nhập hoặc mật khẩu không đúng.",
  "no login in progress": "Chưa có quá trình đăng nhập. Hãy đăng nhập lại.",
  "wrong code": "Mã xác thực không đúng.",
  "current password is wrong": "Mật khẩu hiện tại không đúng.",
  "two-factor authentication is already on": "Xác thực hai bước đã được bật.",
  "no two-factor setup in progress": "Chưa bắt đầu thiết lập xác thực hai bước.",
  "start two-factor setup first": "Hãy bắt đầu thiết lập xác thực hai bước trước.",
  "wrong code; check the time on your device": "Mã không đúng. Hãy kiểm tra thời gian trên thiết bị.",
  "password is wrong": "Mật khẩu không đúng.",
  "role must be admin or viewer": "Vai trò phải là quản trị viên hoặc người xem.",
  "that username is taken": "Tên đăng nhập đã được sử dụng.",
  "bad user id": "Mã người dùng không hợp lệ.",
  "no such user": "Không tìm thấy người dùng.",
  "you cannot change your own role": "Bạn không thể đổi vai trò của chính mình.",
  "you cannot delete yourself": "Bạn không thể xóa tài khoản của chính mình.",
  "no such endpoint": "Không tìm thấy địa chỉ API.",
  "method not allowed": "Phương thức yêu cầu không được phép.",
  "internal error": "Lỗi máy chủ. Vui lòng thử lại.",
  "expected application/json": "Yêu cầu phải sử dụng định dạng application/json.",
  "not found": "Không tìm thấy dữ liệu.",
  "streaming unsupported": "Máy chủ không hỗ trợ truyền dữ liệu trực tiếp.",
  "metrics require a bearer token or a session": "Dữ liệu giám sát cần mã truy cập hoặc phiên đăng nhập.",
  "name is required": "Bạn cần nhập tên thiết bị.",
  "name must be 64 characters or fewer": "Tên thiết bị không được dài quá 64 ký tự.",
  "that is the server's own public key": "Đây là khóa công khai của máy chủ. Hãy dùng khóa của thiết bị.",
  "a peer with that public key already exists": "Đã có thiết bị sử dụng khóa công khai này.",
  "IPv6 is not enabled on this server (set VPN20_SUBNET6)": "Máy chủ chưa bật IPv6. Hãy cấu hình VPN20_SUBNET6.",
  "keepalive must be 0-65535 seconds": "Thời gian duy trì kết nối phải trong khoảng 0–65.535 giây.",
  "MTU must be 0 (server default) or 1280-9000": "MTU phải là 0 (dùng mặc định máy chủ) hoặc trong khoảng 1.280–9.000.",
  "notes must be 2000 characters or fewer": "Ghi chú không được dài quá 2.000 ký tự.",
  "this peer's keys are managed by the client; create a new peer instead": "Thiết bị tự quản lý khóa. Hãy tạo thiết bị mới để thay khóa.",
  "no QR code: the private key is held by the client": "Không có mã QR vì thiết bị tự giữ khóa riêng.",
  "endpoint host is required": "Bạn cần nhập tên miền hoặc IP công khai.",
  "endpoint host must be a hostname or IP address without a port": "Địa chỉ kết nối phải là tên miền hoặc IP, không kèm cổng.",
  "endpoint port must be 1-65535": "Cổng kết nối phải trong khoảng 1–65.535.",
  "MTU must be between 1280 and 9000": "MTU phải trong khoảng 1.280–9.000.",
  "connected window must be 30-3600 seconds": "Thời gian ghi nhận kết nối phải trong khoảng 30–3.600 giây.",
  "at least one route is required": "Bạn cần khai báo ít nhất một tuyến mạng.",
  "password is too long": "Mật khẩu quá dài.",
  "bad secret": "Khóa xác thực không hợp lệ.",
  "invalid public key": "Khóa công khai không hợp lệ."
};

const technicalMessages: Record<string, string> = {
  "peers cannot reach anything beyond the server without forwarding": "Thiết bị không thể truy cập mạng ngoài máy chủ nếu chưa bật chuyển tiếp IP.",
  "strict rp_filter drops legitimate tunnel replies": "rp_filter ở chế độ nghiêm ngặt sẽ loại bỏ các gói phản hồi VPN hợp lệ.",
  "larger UDP receive buffers stop bursts being dropped before WireGuard reads them": "Bộ đệm nhận UDP lớn hơn giúp tránh mất các đợt dữ liệu trước khi WireGuard đọc.",
  "larger UDP send buffers keep the encrypt path from stalling": "Bộ đệm gửi UDP lớn hơn giúp quá trình mã hóa không bị gián đoạn.",
  "default socket receive buffer": "Bộ đệm nhận mặc định của socket.",
  "default socket send buffer": "Bộ đệm gửi mặc định của socket.",
  "deeper per-CPU input queue for 10GbE bursts": "Hàng đợi đầu vào lớn hơn cho mỗi CPU khi nhận các đợt dữ liệu 10GbE.",
  "minimum UDP receive buffer under memory pressure": "Bộ đệm nhận UDP tối thiểu khi thiếu bộ nhớ.",
  "minimum UDP send buffer under memory pressure": "Bộ đệm gửi UDP tối thiểu khi thiếu bộ nhớ.",
  "IPv6 peers cannot reach anything beyond the server without forwarding": "Thiết bị IPv6 không thể truy cập mạng ngoài máy chủ nếu chưa bật chuyển tiếp IP."
};

const errorPatterns: [RegExp, (...parts: string[]) => string][] = [
  [/^client routes: (.+)$/, (detail) => `Định tuyến thiết bị: ${localizeError(detail)}`],
  [/^(IPv4|IPv6): (.+)$/, (family, detail) => `${family}: ${localizeError(detail)}`],
  [/^password must be at least (\d+) characters$/, (n) => `Mật khẩu phải có ít nhất ${n} ký tự.`],
  [/^no free addresses left in (.+)$/, (subnet) => `Mạng ${subnet} không còn địa chỉ trống.`],
  [/^(.+) is outside (.+)$/, (ip, subnet) => `Địa chỉ ${ip} nằm ngoài mạng ${subnet}.`],
  [/^(.+) is reserved for the server$/, (ip) => `Địa chỉ ${ip} được dành cho máy chủ.`],
  [/^(.+) is already assigned$/, (ip) => `Địa chỉ ${ip} đã được cấp.`],
  [/^(.+) is not an IP address$/, (value) => `${value} không phải địa chỉ IP hợp lệ.`],
  [/^(.+) is not a CIDR$/, (value) => `${value} không phải mạng CIDR hợp lệ.`],
  [/^DNS entry (.+) is neither an address nor a domain$/, (value) => `Mục DNS ${value} không phải địa chỉ IP hoặc tên miền hợp lệ.`],
  [/^public key: (.+)$/, () => "Khóa công khai không hợp lệ. Hãy nhập khóa WireGuard ở dạng base64, dài 44 ký tự."],
  [/^bad JSON: (.+)$/, () => "Dữ liệu JSON không hợp lệ. Hãy kiểm tra thông tin đã nhập."],
  [/^settings saved but firewall rules failed: (.+)$/, (detail) => `Đã lưu cài đặt nhưng không áp dụng được tường lửa: ${technicalMessage(detail)}`],
];

export function localizeError(message: string, status?: number): string {
  const text = message.trim();
  if (errorMessages[text]) return errorMessages[text];
  if (text.includes("\n")) return text.split("\n").map((line) => localizeError(line, status)).join("\n");
  for (const [pattern, translate] of errorPatterns) {
    const match = pattern.exec(text);
    if (match) return translate(...match.slice(1));
  }
  // Thông báo đã là tiếng Việt thì giữ nguyên, không thay đổi dữ liệu người dùng.
  if (/[À-ỹ]/u.test(text)) return text;
  if (status === 401) return "Bạn chưa đăng nhập hoặc phiên đăng nhập đã hết hạn.";
  if (status === 403) return "Bạn không có quyền thực hiện thao tác này.";
  if (status === 404) return "Không tìm thấy dữ liệu.";
  if (status === 429) return "Bạn đã thử quá nhiều lần. Vui lòng thử lại sau.";
  if (status && status >= 500) return "Máy chủ gặp lỗi. Vui lòng thử lại sau.";
  return "Không thể hoàn tất yêu cầu. Hãy kiểm tra thông tin và thử lại.";
}

export function technicalMessage(message?: string): string {
  if (!message) return "";
  if (technicalMessages[message]) return technicalMessages[message];
  if (errorMessages[message]) return errorMessages[message];
  if (/[À-ỹ]/u.test(message)) return message;
  const translations: [string, string][] = [
    ["permission denied", "không đủ quyền"],
    ["read-only file system", "hệ thống tệp chỉ đọc"],
    ["no such file or directory", "không tìm thấy tệp hoặc thư mục"],
    ["operation not permitted", "thao tác không được phép"],
    ["cannot allocate memory", "không thể cấp phát bộ nhớ"],
    ["invalid argument", "tham số không hợp lệ"],
  ];
  let localized = message;
  for (const [source, translation] of translations) localized = localized.replaceAll(source, translation);
  return localized === message ? `Chi tiết hệ thống: ${message}` : localized;
}

const auditActions: Record<string, string> = {
  setup: "Thiết lập hệ thống",
  login: "Đăng nhập",
  "login.failed": "Đăng nhập thất bại",
  "login.totp_failed": "Xác thực hai bước thất bại",
  logout: "Đăng xuất",
  "password.changed": "Đổi mật khẩu",
  "password.reset": "Đặt lại mật khẩu",
  "totp.enabled": "Bật xác thực hai bước",
  "totp.disabled": "Tắt xác thực hai bước",
  "sessions.revoked": "Đăng xuất các phiên khác",
  "user.created": "Tạo người dùng",
  "user.updated": "Cập nhật người dùng",
  "user.deleted": "Xóa người dùng",
  "peer.created": "Tạo thiết bị",
  "peer.updated": "Cập nhật thiết bị",
  "peer.deleted": "Xóa thiết bị",
  "peer.enabled": "Bật thiết bị",
  "peer.disabled": "Vô hiệu hóa thiết bị",
  "peer.session_reset": "Đặt lại phiên thiết bị",
  "peer.keys_rotated": "Thay khóa thiết bị",
  "peer.config_viewed": "Xem cấu hình thiết bị",
  "settings.updated": "Cập nhật cài đặt",
};

export function auditAction(action: string): string {
  return auditActions[action] ?? action;
}

export function auditDetail(detail: string): string {
  const details: Record<string, string> = {
    "first administrator created": "Đã tạo quản trị viên đầu tiên",
    "recovery code used": "Đã sử dụng mã khôi phục",
    "role admin": "Vai trò quản trị viên",
    "role viewer": "Vai trò người xem",
    "password reset": "Đã đặt lại mật khẩu",
    "two-factor reset": "Đã đặt lại xác thực hai bước",
    "two-factor cleared": "Đã xóa xác thực hai bước",
    "sessions dropped": "Đã kết thúc các phiên đăng nhập",
  };
  return detail.split(", ").map((part) => details[part] ?? part).join(", ");
}
