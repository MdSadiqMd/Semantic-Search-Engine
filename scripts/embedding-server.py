#!/usr/bin/env python3
import os
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from sentence_transformers import SentenceTransformer

MODEL_NAME = os.getenv("MODEL_NAME", "google/embeddinggemma-300m")
MODEL_CACHE_DIR = os.getenv("MODEL_CACHE_DIR", "/app/models")
SERVER_HOST = os.getenv("SERVER_HOST", "0.0.0.0")
SERVER_PORT = int(os.getenv("SERVER_PORT", "8080"))

os.makedirs(MODEL_CACHE_DIR, exist_ok=True)

try:
    model = SentenceTransformer(MODEL_NAME, cache_folder=MODEL_CACHE_DIR)
except Exception as e:
    raise RuntimeError(f"Failed to load model {MODEL_NAME}: {e}")

app = FastAPI(title="EmbeddingGemma Service")

class EmbedRequest(BaseModel):
    texts: list[str]

class EmbedResponse(BaseModel):
    embeddings: list[list[float]]

@app.post("/embed", response_model=EmbedResponse)
async def embed(req: EmbedRequest):
    if not req.texts:
        raise HTTPException(status_code=400, detail="No texts provided")
    embeddings = model.encode(req.texts, normalize_embeddings=True, convert_to_numpy=True)
    return EmbedResponse(embeddings=embeddings.tolist())

@app.get("/")
async def root():
    return {"status": "ready", "model": MODEL_NAME}

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host=SERVER_HOST, port=SERVER_PORT)
