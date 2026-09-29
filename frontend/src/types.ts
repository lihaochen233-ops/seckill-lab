export interface User {
  id: number;
  email: string;
  name: string;
  role: "admin" | "customer";
}
export interface Session {
  user: User;
  csrf_token: string;
  expires_at: number;
}
export interface Product {
  id: number;
  name: string;
  subtitle: string;
  description: string;
  category: string;
  image: string;
  price_cents: number;
  original_price_cents: number;
  stock: number;
  initial_stock: number;
  status: string;
  featured: boolean;
  created_at: number;
}
export interface Address {
  id: number;
  recipient: string;
  phone: string;
  region: string;
  detail: string;
}
export interface CartItem {
  product: Product;
  quantity: number;
}
export interface OrderItem {
  product_id: number;
  name: string;
  image: string;
  price_cents: number;
  quantity: number;
}
export interface Order {
  id: string;
  user_id: number;
  kind: string;
  status: string;
  total_cents: number;
  items: OrderItem[];
  address: Address;
  activity_id: number;
  ticket_id: string;
  created_at: number;
  expires_at: number;
  paid_at: number;
  tracking: string;
  replayed?: boolean;
}
export interface Activity {
  id: number;
  product_id: number;
  name: string;
  image: string;
  price_cents: number;
  original_price_cents: number;
  initial_stock: number;
  stock: number;
  returned: number;
  starts_at: number;
  ends_at: number;
  status: string;
  available: number;
  ready: boolean;
}
export interface Ticket {
  id: string;
  activity_id: number;
  status: string;
  reason: string;
  order_id?: string;
  created_at: number;
}
export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  size: number;
}
