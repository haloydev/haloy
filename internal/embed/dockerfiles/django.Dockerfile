FROM python:{{.PythonVersion}}-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libpq-dev \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
RUN pip install --no-cache-dir gunicorn

# Copy application code
COPY . .

# Collect static files
RUN python manage.py collectstatic --noinput

# Create non-root user
RUN addgroup --system --gid 1001 django
RUN adduser --system --uid 1001 --ingroup django django
RUN chown -R django:django /app
USER django

ENV PORT={{.Port}}
ENV DJANGO_PROJECT={{.ProjectName}}
EXPOSE ${PORT}

# Uncomment and configure once you have a health check endpoint:
# HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
#   CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:${PORT}/health/')" || exit 1

CMD gunicorn --bind 0.0.0.0:${PORT} ${DJANGO_PROJECT}.wsgi:application
