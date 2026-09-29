package mall

import (
	"context"
	"fmt"
	"time"
)

// Seed 只在空商城中创建演示目录；不会覆盖已有商品、用户或库存。
func (d *DB) Seed(ctx context.Context, adminEmail, adminPassword string, demo bool) ([]int64, error) {
	var count int
	if err := d.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM mall_users WHERE role='admin'").Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		if adminEmail == "" || len(adminPassword) < 12 {
			return nil, fmt.Errorf("首次初始化必须设置 ADMIN_EMAIL 和至少 12 字节的 ADMIN_PASSWORD")
		}
		if _, err := d.CreateUser(ctx, adminEmail, "商城管理员", adminPassword, "admin"); err != nil {
			return nil, err
		}
	}
	if demo {
		if err := d.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM mall_users WHERE email='demo@pulse.local'").Scan(&count); err != nil {
			return nil, err
		}
		if count == 0 {
			u, err := d.CreateUser(ctx, "demo@pulse.local", "生活体验官", "PulseDemo2026!", "customer")
			if err != nil {
				return nil, err
			}
			if _, err = d.SaveAddress(ctx, u.ID, Address{Recipient: "体验官", Phone: "13800000000", Region: "浙江省 杭州市 西湖区", Detail: "脉冲生活体验中心 101 室（演示地址）"}); err != nil {
				return nil, err
			}
		}
	}
	if err := d.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM mall_products").Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, nil
	}
	products := []Product{
		{Name: "AIR 75 无线机械键盘", Subtitle: "让每一次敲击，都有好心情", Description: "75% 精简布局，保留完整方向键。三模连接与柔和的奶油配色，让工作桌面安静而有序。\n\n套装包含：键盘、接收器、连接线、拔键器。此为教学商城演示商品。", Category: "桌面数码", Image: "keyboard", Price: 39900, OriginalPrice: 59900, Stock: 500, Featured: true},
		{Name: "HUSH 降噪无线耳机", Subtitle: "把喧嚣留在音乐之外", Description: "轻盈头戴设计，柔软耳垫。适合通勤、阅读与专注工作的日常陪伴。\n\n雾白与薄荷绿的灵感配色，收纳袋随盒附赠。此为教学商城演示商品。", Category: "桌面数码", Image: "headphones", Price: 59900, OriginalPrice: 79900, Stock: 400, Featured: true},
		{Name: "HALO 柔光桌面灯", Subtitle: "给夜晚留一盏温柔的光", Description: "圆弧灯罩与细腻哑光底座，点亮书桌一角。三档色温与无级调光，让每种心情都有合适的亮度。此为教学商城演示商品。", Category: "品质生活", Image: "lamp", Price: 22900, OriginalPrice: 29900, Stock: 300, Featured: true},
		{Name: "WAVE 便携蓝牙音箱", Subtitle: "喜欢的旋律，随你出发", Description: "小巧织物外观与便携挂绳。放在书桌，或带去周末野餐，让音乐融入生活。此为教学商城演示商品。", Category: "桌面数码", Image: "speaker", Price: 19900, OriginalPrice: 25900, Stock: 400, Featured: true},
		{Name: "ROAM 城市通勤背包", Subtitle: "轻装出门，装下所有可能", Description: "简洁线条与分层收纳，日常电脑、书本与随身物件各有位置。防泼水面料适应城市通勤。此为教学商城演示商品。", Category: "通勤随行", Image: "bag", Price: 26900, OriginalPrice: 35900, Stock: 300, Featured: true},
		{Name: "SIP 随行保温杯", Subtitle: "把温度握在手心", Description: "圆润握感，轻巧杯身，简洁旋盖。让晨间咖啡和午后热茶在路上继续陪伴你。此为教学商城演示商品。", Category: "通勤随行", Image: "bottle", Price: 12900, OriginalPrice: 16900, Stock: 500, Featured: false},
		{Name: "TEMPO 极简腕表", Subtitle: "让时间，也有自己的节奏", Description: "低饱和表盘与柔软表带，简约而耐看。记录生活的分秒，不打扰你的专注。此为教学商城演示商品。", Category: "通勤随行", Image: "watch", Price: 32900, OriginalPrice: 42900, Stock: 200, Featured: false},
		{Name: "MOMENT 口袋相机", Subtitle: "收藏生活里闪光的瞬间", Description: "复古灵感与轻量设计，用镜头记录散步、旅途和朋友的笑容。此为教学商城演示商品，参数与售价用于学习演示。", Category: "品质生活", Image: "camera", Price: 89900, OriginalPrice: 119900, Stock: 150, Featured: false},
		{Name: "GLIDE 静音无线鼠标", Subtitle: "掌心轻盈，工作从容", Description: "贴合掌心的弧线，细腻触感与低噪按键。三档灵敏度切换，让日常办公更自在。此为教学商城演示商品。", Category: "桌面数码", Image: "mouse", Price: 15900, OriginalPrice: 19900, Stock: 500, Featured: true},
	}
	ids := []int64{}
	for i, p := range products {
		p.Status = "active"
		saved, err := d.SaveProduct(ctx, 0, p)
		if err != nil {
			return nil, err
		}
		if i < 3 {
			start := nowMS() - int64(time.Hour/time.Millisecond)
			end := nowMS() + int64(48*time.Hour/time.Millisecond)
			if i == 2 {
				start = nowMS() + int64(time.Hour/time.Millisecond)
			}
			a, err := d.CreateActivity(ctx, 0, Activity{ProductID: saved.ID, Price: p.Price / 2, Stock: 100, StartsAt: start, EndsAt: end})
			if err != nil {
				return nil, err
			}
			ids = append(ids, a.ID)
		}
	}
	return ids, nil
}
