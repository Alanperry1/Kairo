req<-jsonlite::fromJSON(file('stdin'),simplifyVector=FALSE)
value<-req$inputs$a+as.numeric(req$dataset_rows[[1]]$value)
cat(jsonlite::toJSON(list(kind='computational_experiment',metrics=list(sum=value),findings=as.list(paste('Computed sum',value)),limitations=as.list('Test computation only')),auto_unbox=TRUE))
