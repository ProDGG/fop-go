FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /src
COPY pom.xml .
RUN mvn -q -B dependency:go-offline
COPY src src
RUN mvn -q -B package -DskipTests

FROM eclipse-temurin:17-jre
WORKDIR /app
COPY --from=build /src/target/report-service.jar .
ENV PORT=8080 TEMPLATES_DIR=/app/templates FONTS_DIR=/app/fonts
# mount your stylesheets and fonts:  -v ./templates:/app/templates -v ./fonts:/app/fonts
EXPOSE 8080
USER 10001
ENTRYPOINT ["java", "-Djava.awt.headless=true", "-jar", "report-service.jar"]
