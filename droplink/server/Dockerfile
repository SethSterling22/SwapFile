FROM node:20-alpine

WORKDIR /app

# Copy server dependencies
COPY server/package*.json ./server/
RUN cd server && npm ci --omit=dev

# Copy application files
COPY server/ ./server/
COPY client/ ./client/

WORKDIR /app/server

ENV NODE_ENV=production
ENV PORT=3000

EXPOSE 3000

CMD ["node", "index.js"]
