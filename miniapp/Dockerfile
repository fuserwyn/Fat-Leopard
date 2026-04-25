# Мини-апп на Railway: локальный npm не нужен — всё ставится и собирается внутри `docker build`.
# Runtime: Vite-артефакты + serve (см. scripts/serve.cjs).
FROM node:20-alpine AS build
WORKDIR /app
# URL сервиса с ботом (Railway), без path — в чате фетчит /api/miniapp/messages
ARG VITE_MINIAPP_API_URL
ENV VITE_MINIAPP_API_URL=$VITE_MINIAPP_API_URL
COPY package.json package-lock.json* ./
RUN npm ci
COPY . ./
RUN npm run build
RUN npm prune --omit=dev

FROM node:20-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/dist ./dist
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/scripts ./scripts
COPY --from=build /app/package.json ./
EXPOSE 3000
ENV PORT=3000
CMD ["node", "scripts/serve.cjs"]
