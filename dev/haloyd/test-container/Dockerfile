FROM nginx:alpine

# Copy the main website content
COPY index.html /usr/share/nginx/html/

# Create a health check endpoint
RUN echo "OK" > /usr/share/nginx/html/health

# Enable Docker's built-in healthcheck
HEALTHCHECK --interval=5s --timeout=3s --retries=3 \
  CMD wget -q --spider http://localhost/health || exit 1

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
