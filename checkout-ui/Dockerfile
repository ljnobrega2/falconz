# Stage 1: build React SPA (checkout público FALK)
FROM node:20-alpine AS builder
WORKDIR /app
ARG VITE_GA4_MEASUREMENT_ID="G-22GCCY3KMP"
ENV VITE_GA4_MEASUREMENT_ID=$VITE_GA4_MEASUREMENT_ID
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# Stage 2: nginx serve estático
FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
