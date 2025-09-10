#!/usr/bin/env python3
import os
import sys
import time
import logging
from typing import List, Dict, Any, Optional
import uvicorn
from fastapi import FastAPI, HTTPException, BackgroundTasks
from pydantic import BaseModel, Field
import torch
from sentence_transformers import SentenceTransformer
import numpy as np
from datetime import datetime

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

MODEL_NAME = os.getenv("MODEL_NAME", "google/embeddinggemma-300m")
MODEL_CACHE_DIR = os.getenv("MODEL_CACHE_DIR", "./models")
SERVER_HOST = os.getenv("SERVER_HOST", "0.0.0.0")
SERVER_PORT = int(os.getenv("SERVER_PORT", "8080"))
MAX_BATCH_SIZE = int(os.getenv("MAX_BATCH_SIZE", "32"))
MAX_SEQUENCE_LENGTH = int(os.getenv("MAX_SEQUENCE_LENGTH", "512"))

model = None
device = None
model_dimensions = 768
startup_time = None

class EmbeddingRequest(BaseModel):
    text: str = Field(..., description="Text to generate embedding for", max_length=8192)
    model: str = Field(default=MODEL_NAME, description="Model to use for embedding")
    
class BatchEmbeddingRequest(BaseModel):
    texts: List[str] = Field(..., description="List of texts to generate embeddings for", max_items=MAX_BATCH_SIZE)
    model: str = Field(default=MODEL_NAME, description="Model to use for embedding")

class EmbeddingResponse(BaseModel):
    embedding: List[float] = Field(..., description="Generated embedding vector")
    model: str = Field(..., description="Model used for generation")
    dimensions: int = Field(..., description="Embedding dimensions")
    
class BatchEmbeddingResponse(BaseModel):
    embeddings: List[List[float]] = Field(..., description="Generated embedding vectors")
    model: str = Field(..., description="Model used for generation")
    dimensions: int = Field(..., description="Embedding dimensions")
    count: int = Field(..., description="Number of embeddings generated")

class HealthResponse(BaseModel):
    status: str = Field(..., description="Service status")
    model: str = Field(..., description="Loaded model")
    device: str = Field(..., description="Device being used")
    dimensions: int = Field(..., description="Embedding dimensions")
    uptime_seconds: float = Field(..., description="Service uptime in seconds")
    memory_usage_mb: Optional[float] = Field(None, description="Memory usage in MB")

class ErrorResponse(BaseModel):
    error: str = Field(..., description="Error message")
    detail: Optional[str] = Field(None, description="Error details")

app = FastAPI(
    title="EmbeddingGemma Server",
    description="Local embedding generation service using Google's EmbeddingGemma model",
    version="1.0.0",
    docs_url="/docs",
    redoc_url="/redoc"
)

def get_memory_usage():
    """Get current memory usage in MB."""
    try:
        import psutil
        process = psutil.Process(os.getpid())
        return process.memory_info().rss / 1024 / 1024
    except ImportError:
        return None

def load_model():
    """Load the EmbeddingGemma model."""
    global model, device, model_dimensions, startup_time
    
    startup_time = time.time()
    
    logger.info(f"Loading EmbeddingGemma model: {MODEL_NAME}")
    logger.info(f"Cache directory: {MODEL_CACHE_DIR}")
    
    os.environ['TRANSFORMERS_CACHE'] = MODEL_CACHE_DIR
    os.environ['HF_HOME'] = MODEL_CACHE_DIR

    device = "cuda" if torch.cuda.is_available() else "cpu"
    logger.info(f"Using device: {device}")
    
    if device == "cuda":
        logger.info(f"GPU: {torch.cuda.get_device_name()}")
        logger.info(f"GPU Memory: {torch.cuda.get_device_properties(0).total_memory / 1024**3:.1f} GB")
    
    try:
        model = SentenceTransformer(MODEL_NAME, device=device)
        
        test_embedding = model.encode("test", convert_to_numpy=True)
        model_dimensions = len(test_embedding)
        
        logger.info(f"Model loaded successfully!")
        logger.info(f"Model dimensions: {model_dimensions}")
        logger.info(f"Model max sequence length: {model.max_seq_length}")
        
        return True
        
    except Exception as e:
        logger.error(f"Failed to load model: {e}")
        return False

def preprocess_text(text: str) -> str:
    """Preprocess text for embedding generation."""
    if len(text) > MAX_SEQUENCE_LENGTH * 4:
        text = text[:MAX_SEQUENCE_LENGTH * 4]
        logger.warning(f"Text truncated to {len(text)} characters")

    text = text.strip()
    if not text:
        text = "empty"
    
    return text

@app.on_event("startup")
async def startup_event():
    """Initialize the model on startup."""
    logger.info("Starting EmbeddingGemma server...")
    
    if not load_model():
        logger.error("Failed to load model. Server will not function properly.")
        sys.exit(1)
    
    logger.info("EmbeddingGemma server started successfully!")

@app.get("/health", response_model=HealthResponse)
async def health_check():
    """Health check endpoint."""
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")
    
    uptime = time.time() - startup_time if startup_time else 0
    memory_usage = get_memory_usage()
    
    return HealthResponse(
        status="healthy",
        model=MODEL_NAME,
        device=device,
        dimensions=model_dimensions,
        uptime_seconds=uptime,
        memory_usage_mb=memory_usage
    )

@app.post("/v1/embeddings", response_model=EmbeddingResponse)
async def generate_embedding(request: EmbeddingRequest):
    """Generate embedding for a single text."""
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")
    
    try:
        text = preprocess_text(request.text)
        
        start_time = time.time()
        embedding = model.encode(text, convert_to_numpy=True, normalize_embeddings=True)
        generation_time = time.time() - start_time
        
        logger.info(f"Generated embedding in {generation_time:.3f}s for text length {len(text)}")
        
        return EmbeddingResponse(
            embedding=embedding.tolist(),
            model=MODEL_NAME,
            dimensions=len(embedding)
        )
        
    except Exception as e:
        logger.error(f"Error generating embedding: {e}")
        raise HTTPException(status_code=500, detail=f"Failed to generate embedding: {str(e)}")

@app.post("/v1/embeddings/batch", response_model=BatchEmbeddingResponse)
async def generate_batch_embeddings(request: BatchEmbeddingRequest):
    """Generate embeddings for multiple texts."""
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")
    
    if len(request.texts) > MAX_BATCH_SIZE:
        raise HTTPException(
            status_code=400, 
            detail=f"Batch size {len(request.texts)} exceeds maximum {MAX_BATCH_SIZE}"
        )
    
    try:
        texts = [preprocess_text(text) for text in request.texts]
        
        start_time = time.time()
        embeddings = model.encode(texts, convert_to_numpy=True, normalize_embeddings=True)
        generation_time = time.time() - start_time
        
        logger.info(f"Generated {len(embeddings)} embeddings in {generation_time:.3f}s")
        
        return BatchEmbeddingResponse(
            embeddings=embeddings.tolist(),
            model=MODEL_NAME,
            dimensions=len(embeddings[0]) if len(embeddings) > 0 else model_dimensions,
            count=len(embeddings)
        )
        
    except Exception as e:
        logger.error(f"Error generating batch embeddings: {e}")
        raise HTTPException(status_code=500, detail=f"Failed to generate embeddings: {str(e)}")

@app.post("/v1/similarity")
async def calculate_similarity(texts: List[str]):
    """Calculate similarity between texts."""
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")
    
    if len(texts) < 2:
        raise HTTPException(status_code=400, detail="At least 2 texts required for similarity calculation")
    
    try:
        embeddings = model.encode(texts, convert_to_numpy=True, normalize_embeddings=True)
        
        similarity_matrix = np.dot(embeddings, embeddings.T)
        
        return {
            "similarity_matrix": similarity_matrix.tolist(),
            "texts": texts,
            "model": MODEL_NAME
        }
        
    except Exception as e:
        logger.error(f"Error calculating similarity: {e}")
        raise HTTPException(status_code=500, detail=f"Failed to calculate similarity: {str(e)}")

@app.get("/models")
async def list_models():
    """List available models."""
    return {
        "models": [
            {
                "id": MODEL_NAME,
                "name": MODEL_NAME,
                "dimensions": model_dimensions,
                "max_sequence_length": MAX_SEQUENCE_LENGTH,
                "description": "Google EmbeddingGemma model for code understanding"
            }
        ]
    }

@app.get("/stats")
async def get_stats():
    """Get server statistics."""
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")
    
    uptime = time.time() - startup_time if startup_time else 0
    memory_usage = get_memory_usage()
    
    stats = {
        "model": MODEL_NAME,
        "device": device,
        "dimensions": model_dimensions,
        "uptime_seconds": uptime,
        "memory_usage_mb": memory_usage,
        "max_batch_size": MAX_BATCH_SIZE,
        "max_sequence_length": MAX_SEQUENCE_LENGTH
    }
    
    if device == "cuda":
        try:
            stats["gpu_memory_allocated"] = torch.cuda.memory_allocated() / 1024**3
            stats["gpu_memory_cached"] = torch.cuda.memory_reserved() / 1024**3
        except:
            pass
    
    return stats

@app.exception_handler(HTTPException)
async def http_exception_handler(request, exc):
    return {"error": exc.detail, "status_code": exc.status_code}

@app.exception_handler(Exception)
async def general_exception_handler(request, exc):
    logger.error(f"Unhandled exception: {exc}")
    return {"error": "Internal server error", "detail": str(exc)}

@app.get("/")
async def root():
    """Root endpoint."""
    return {"message": "EmbeddingGemma Server", "status": "running", "model": MODEL_NAME}

if __name__ == "__main__":
    os.makedirs(MODEL_CACHE_DIR, exist_ok=True)
    
    print(f"""
🚀 Starting EmbeddingGemma Server
=================================
Model: {MODEL_NAME}
Cache: {MODEL_CACHE_DIR}
Host: {SERVER_HOST}
Port: {SERVER_PORT}
Device: {'CUDA' if torch.cuda.is_available() else 'CPU'}
""")
    uvicorn.run(
        "embedding-server:app",
        host=SERVER_HOST,
        port=SERVER_PORT,
        reload=False,
        log_level="info",
        access_log=True
    )
