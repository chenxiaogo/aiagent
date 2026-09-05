# 构建阶段
FROM node:20-alpine AS builder

WORKDIR /app

# 复制 package.json 并安装依赖
COPY package*.json ./
RUN npm install --registry=https://registry.npmmirror.com

# 复制源码
COPY . .

# 构建
RUN npm run build

# 运行阶段（Nginx）
FROM nginx:alpine

# 复制构建产物
COPY --from=builder /app/dist /usr/share/nginx/html

# 复制 Nginx 配置
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

# HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
#     CMD wget --no-verbose --tries=1 --spider http://localhost/ || exit 1

CMD ["nginx", "-g", "daemon off;"]
