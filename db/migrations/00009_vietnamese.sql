-- Vietnamese-only product (2026-10-31): capability titles are shown in the
-- dashboard (shop capabilities, platform admin), so the seeded English titles
-- become Vietnamese. Keys, modes and doc references stay unchanged.

-- +goose Up
UPDATE capability_definitions SET title = 'Đọc thông tin shop' WHERE key = 'shop.info.read';
UPDATE capability_definitions SET title = 'Đọc hồ sơ shop' WHERE key = 'shop.profile.read';
UPDATE capability_definitions SET title = 'Cập nhật tên và mô tả shop' WHERE key = 'shop.profile.update';
UPDATE capability_definitions SET title = 'Tải lên logo shop' WHERE key = 'shop.avatar.upload';
UPDATE capability_definitions SET title = 'Tải lên banner shop' WHERE key = 'shop.banner.upload';
UPDATE capability_definitions SET title = 'Cập nhật trang trí shop' WHERE key = 'shop.decoration.update';
UPDATE capability_definitions SET title = 'Đọc thông tin kho hàng' WHERE key = 'shop.warehouse.read';
UPDATE capability_definitions SET title = 'Cập nhật kho hàng' WHERE key = 'shop.warehouse.update';
UPDATE capability_definitions SET title = 'Đọc địa chỉ lấy hàng và địa chỉ trả hàng' WHERE key = 'shop.address.read';
UPDATE capability_definitions SET title = 'Sửa một địa chỉ có sẵn' WHERE key = 'shop.address.update';
UPDATE capability_definitions SET title = 'Tạo địa chỉ mới' WHERE key = 'shop.address.create';
UPDATE capability_definitions SET title = 'Đặt địa chỉ mặc định, địa chỉ lấy hàng và địa chỉ trả hàng' WHERE key = 'shop.address.config';
UPDATE capability_definitions SET title = 'Đọc đơn vị vận chuyển' WHERE key = 'shop.logistics.read';
UPDATE capability_definitions SET title = 'Bật hoặc tắt đơn vị vận chuyển và COD' WHERE key = 'shop.logistics.update';
UPDATE capability_definitions SET title = 'Đọc ngành hàng' WHERE key = 'product.category.read';
UPDATE capability_definitions SET title = 'Đọc thuộc tính và thương hiệu của ngành hàng' WHERE key = 'product.attribute.read';
UPDATE capability_definitions SET title = 'Đọc giới hạn sản phẩm' WHERE key = 'product.limits.read';
UPDATE capability_definitions SET title = 'Tải lên hình ảnh sản phẩm' WHERE key = 'product.image.upload';
UPDATE capability_definitions SET title = 'Tạo sản phẩm' WHERE key = 'product.create';
UPDATE capability_definitions SET title = 'Tạo phân loại hàng' WHERE key = 'product.variation.create';
UPDATE capability_definitions SET title = 'Cập nhật sản phẩm' WHERE key = 'product.update';
UPDATE capability_definitions SET title = 'Cập nhật giá' WHERE key = 'product.price.update';
UPDATE capability_definitions SET title = 'Cập nhật tồn kho' WHERE key = 'product.stock.update';
UPDATE capability_definitions SET title = 'Đọc sản phẩm' WHERE key = 'product.read';
UPDATE capability_definitions SET title = 'Tạo sản phẩm hàng loạt' WHERE key = 'product.batch_create';

-- +goose Down
UPDATE capability_definitions SET title = 'Read shop information' WHERE key = 'shop.info.read';
UPDATE capability_definitions SET title = 'Read shop profile' WHERE key = 'shop.profile.read';
UPDATE capability_definitions SET title = 'Update shop name and description' WHERE key = 'shop.profile.update';
UPDATE capability_definitions SET title = 'Upload shop logo' WHERE key = 'shop.avatar.upload';
UPDATE capability_definitions SET title = 'Upload shop banner' WHERE key = 'shop.banner.upload';
UPDATE capability_definitions SET title = 'Update shop decoration' WHERE key = 'shop.decoration.update';
UPDATE capability_definitions SET title = 'Read warehouses' WHERE key = 'shop.warehouse.read';
UPDATE capability_definitions SET title = 'Update warehouses' WHERE key = 'shop.warehouse.update';
UPDATE capability_definitions SET title = 'Read pickup and return addresses' WHERE key = 'shop.address.read';
UPDATE capability_definitions SET title = 'Edit an existing address' WHERE key = 'shop.address.update';
UPDATE capability_definitions SET title = 'Create an address' WHERE key = 'shop.address.create';
UPDATE capability_definitions SET title = 'Set default, pickup and return address' WHERE key = 'shop.address.config';
UPDATE capability_definitions SET title = 'Read shipping channels' WHERE key = 'shop.logistics.read';
UPDATE capability_definitions SET title = 'Enable or disable shipping channels and COD' WHERE key = 'shop.logistics.update';
UPDATE capability_definitions SET title = 'Read product categories' WHERE key = 'product.category.read';
UPDATE capability_definitions SET title = 'Read category attributes and brands' WHERE key = 'product.attribute.read';
UPDATE capability_definitions SET title = 'Read product limits' WHERE key = 'product.limits.read';
UPDATE capability_definitions SET title = 'Upload product images' WHERE key = 'product.image.upload';
UPDATE capability_definitions SET title = 'Create products' WHERE key = 'product.create';
UPDATE capability_definitions SET title = 'Create product variations' WHERE key = 'product.variation.create';
UPDATE capability_definitions SET title = 'Update products' WHERE key = 'product.update';
UPDATE capability_definitions SET title = 'Update prices' WHERE key = 'product.price.update';
UPDATE capability_definitions SET title = 'Update stock' WHERE key = 'product.stock.update';
UPDATE capability_definitions SET title = 'Read products' WHERE key = 'product.read';
UPDATE capability_definitions SET title = 'Create products in batches' WHERE key = 'product.batch_create';
