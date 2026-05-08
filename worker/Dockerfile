FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
ENV PIP_DISABLE_PIP_VERSION_CHECK=1
ENV PIP_DEFAULT_TIMEOUT=120
RUN pip install --no-cache-dir --retries 5 --timeout 120 -r requirements.txt

ENV PYTHONUNBUFFERED=1

COPY . .

CMD ["python", "main.py"]
